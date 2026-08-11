package app

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

func TestFFmpegManagerDownloadsVerifiesAndReuses(t *testing.T) {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("the prototype download target is Windows x64")
	}
	payload := []byte("test ffmpeg binary")
	hash := sha256.Sum256(payload)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		response.Header().Set("Content-Type", "application/gzip")
		writer := gzip.NewWriter(response)
		_, _ = writer.Write(payload)
		_ = writer.Close()
	}))
	defer server.Close()

	var mu sync.Mutex
	states := []string{}
	path := filepath.Join(t.TempDir(), "ffmpeg.exe")
	manager := newFFmpegManagerWithOptions(ffmpegOptions{
		URL: server.URL, SHA256: hex.EncodeToString(hash[:]), Path: path, Client: server.Client(),
		Emit: func(status FFmpegStatus) {
			mu.Lock()
			states = append(states, status.State)
			mu.Unlock()
		},
	})

	installed, err := manager.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if installed != path {
		t.Fatalf("installed path = %q", installed)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(payload) {
		t.Fatalf("installed payload = %q, err = %v", data, err)
	}
	if _, err := manager.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(states) == 0 || states[len(states)-1] != "ready" {
		t.Fatalf("states = %v", states)
	}
}

func TestFFmpegManagerRejectsBadChecksumAndCleansPart(t *testing.T) {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("the prototype download target is Windows x64")
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		writer := gzip.NewWriter(response)
		_, _ = writer.Write([]byte("tampered"))
		_ = writer.Close()
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "ffmpeg.exe")
	manager := newFFmpegManagerWithOptions(ffmpegOptions{
		URL: server.URL, SHA256: "0000000000000000000000000000000000000000000000000000000000000000",
		Path: path, Client: server.Client(),
	})
	if _, err := manager.Ensure(context.Background()); err == nil {
		t.Fatal("expected checksum failure")
	}
	if fileExists(path) || fileExists(path+".part") {
		t.Fatal("failed download left files behind")
	}
	if manager.Status().State != "error" {
		t.Fatalf("status = %#v", manager.Status())
	}
}

func TestFFmpegManagerFallsBackAfterPrimaryNetworkFailure(t *testing.T) {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("the prototype download target is Windows x64")
	}
	payload := []byte("fallback ffmpeg binary")
	hash := sha256.Sum256(payload)
	var fallbackRequests atomic.Int32
	fallback := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		fallbackRequests.Add(1)
		writer := gzip.NewWriter(response)
		_, _ = writer.Write(payload)
		_ = writer.Close()
	}))
	defer fallback.Close()
	primary := httptest.NewServer(http.NotFoundHandler())
	primaryURL := primary.URL
	primary.Close()

	manager := newFFmpegManagerWithOptions(ffmpegOptions{
		URL: primaryURL, FallbackURL: fallback.URL,
		SHA256: hex.EncodeToString(hash[:]), Path: filepath.Join(t.TempDir(), "ffmpeg.exe"), Client: fallback.Client(),
	})
	if _, err := manager.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fallbackRequests.Load() != 1 {
		t.Fatalf("fallback requests = %d, want 1", fallbackRequests.Load())
	}
}

func TestDefaultAppPathsSupportsIsolatedDataDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("NONOKA_DATA_DIR", root)
	paths, err := defaultAppPaths()
	if err != nil {
		t.Fatal(err)
	}
	if paths.Root != root || paths.FFmpegDir != filepath.Join(root, "ffmpeg") ||
		paths.TempDir != filepath.Join(root, "temp") || paths.WebviewDir != filepath.Join(root, "webview") {
		t.Fatalf("paths = %#v", paths)
	}
}
