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
	"sync"
	"sync/atomic"
	"testing"
)

func TestFFmpegManagerDownloadsVerifiesAndReuses(t *testing.T) {
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

// 补签名会改动二进制内容，官方包哈希随之对不上；装好后必须认旁车哈希，否则每次启动都重下
func TestFFmpegManagerReusesCodesignedBinary(t *testing.T) {
	payload := []byte("test ffmpeg binary")
	hash := sha256.Sum256(payload)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer := gzip.NewWriter(response)
		_, _ = writer.Write(payload)
		_ = writer.Close()
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "ffmpeg")
	options := func(codesign func(string) error) ffmpegOptions {
		return ffmpegOptions{
			URL: server.URL, SHA256: hex.EncodeToString(hash[:]), Path: path,
			Client: server.Client(), Codesign: codesign,
		}
	}

	// 模拟 codesign：往尾部塞一段签名，文件哈希跟着变
	manager := newFFmpegManagerWithOptions(options(func(target string) error {
		return os.WriteFile(target, append(append([]byte{}, payload...), []byte("-adhoc-signature")...), 0o755)
	}))
	if _, err := manager.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}

	// 重开进程：新 manager 面对的是已签名的文件，应直接判定装好而不是重新下载
	restarted := newFFmpegManagerWithOptions(options(nil))
	if _, err := restarted.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1（签名后被误判损坏，重新下载了）", requests.Load())
	}
	if got := restarted.Status().State; got != "ready" {
		t.Fatalf("state = %q, want ready", got)
	}

	// 二进制被改坏且与旁车哈希不符时，仍应重新下载
	if err := os.WriteFile(path, []byte("corrupted"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := newFFmpegManagerWithOptions(options(nil)).Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d, want 2（损坏的二进制没有被重新下载）", requests.Load())
	}
}
