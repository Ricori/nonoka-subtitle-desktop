package app

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newUploadTestService(t *testing.T, client *http.Client, report func(MediaProgress)) *DesktopService {
	t.Helper()
	service, err := newDesktopService(appPathsAt(t.TempDir()), newFFmpegManagerWithOptions(ffmpegOptions{}), newPrototypeService(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	service.httpClient = client
	service.media = newMediaEngine(service.ffmpeg, service.paths.TempDir, report)
	return service
}

func createUploadTemp(t *testing.T, service *DesktopService, data []byte) string {
	t.Helper()
	file, err := os.CreateTemp(service.paths.TempDir, audioTempPrefix+"test_*.m4a")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	t.Cleanup(func() { _ = os.Remove(path) })
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUploadFileStreamsHeadersAndProgress(t *testing.T) {
	payload := bytes.Repeat([]byte("n"), 512*1024)
	var received []byte
	var contentType, cacheControl string
	var contentLength int64
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		contentType = request.Header.Get("Content-Type")
		cacheControl = request.Header.Get("Cache-Control")
		contentLength = request.ContentLength
		received, _ = io.ReadAll(request.Body)
		response.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	var mu sync.Mutex
	progress := make([]MediaProgress, 0)
	service := newUploadTestService(t, server.Client(), func(value MediaProgress) {
		mu.Lock()
		progress = append(progress, value)
		mu.Unlock()
	})
	path := createUploadTemp(t, service, payload)
	result, err := service.UploadFile("test", path, server.URL+"/signed?key=1", map[string]string{
		"Content-Type": "video/mp4", "Cache-Control": "public, max-age=31536000, immutable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Status != http.StatusCreated || !bytes.Equal(received, payload) {
		t.Fatalf("upload result = %#v, bytes = %d", result, len(received))
	}
	if contentType != "video/mp4" || cacheControl == "" || contentLength != int64(len(payload)) {
		t.Fatalf("headers = %q, %q, %d", contentType, cacheControl, contentLength)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(progress) == 0 || progress[len(progress)-1].Done != float64(len(payload)) || progress[len(progress)-1].Stage != "upload" {
		t.Fatalf("progress = %#v", progress)
	}
}

func TestUploadFileReportsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusForbidden)
		_, _ = response.Write([]byte("signature mismatch"))
	}))
	defer server.Close()
	service := newUploadTestService(t, server.Client(), nil)
	_, err := service.UploadFile("test", createUploadTemp(t, service, []byte("audio")), server.URL, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") || !strings.Contains(err.Error(), "signature mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUploadFileCanBeCancelled(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		select {
		case <-request.Context().Done():
		case <-release:
		}
	}))
	defer func() {
		close(release)
		server.Close()
	}()
	service := newUploadTestService(t, server.Client(), nil)
	path := createUploadTemp(t, service, bytes.Repeat([]byte("x"), 2<<20))
	result := make(chan error, 1)
	go func() {
		_, err := service.UploadFile("cancel", path, server.URL, nil)
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("upload did not start")
	}
	if !service.CancelMediaJob("cancel") {
		t.Fatal("upload job was not canceled")
	}
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "已取消") {
			t.Fatalf("unexpected cancellation error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled upload did not return")
	}
}

func TestUploadThumbUsesSignedHeaders(t *testing.T) {
	var contentType, cacheControl string
	var received []byte
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		contentType = request.Header.Get("Content-Type")
		cacheControl = request.Header.Get("Cache-Control")
		received, _ = io.ReadAll(request.Body)
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	service := newUploadTestService(t, server.Client(), nil)
	service.library = []libraryDiskEntry{{LibraryEntry: LibraryEntry{ID: "thumb", Title: "Thumb"}}}
	want := []byte("jpeg-data")
	if err := os.WriteFile(service.thumbPath("thumb"), want, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := service.UploadThumb("thumb", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || !bytes.Equal(received, want) || contentType != "image/jpeg" || cacheControl != "public, max-age=31536000, immutable" {
		t.Fatalf("result = %#v, headers = %q, %q, body = %q", result, contentType, cacheControl, received)
	}
}

func TestDeleteTempOnlyRemovesOwnedAudio(t *testing.T) {
	service := newUploadTestService(t, http.DefaultClient, nil)
	owned := createUploadTemp(t, service, []byte("audio"))
	outside := filepath.Join(t.TempDir(), audioTempPrefix+"outside.m4a")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !service.DeleteTemp(owned) || fileExists(owned) {
		t.Fatal("owned temporary audio was not deleted")
	}
	if service.DeleteTemp(outside) || !fileExists(outside) {
		t.Fatal("file outside the app temp directory was deleted")
	}
}

func TestValidateUploadURL(t *testing.T) {
	for _, raw := range []string{"file:///tmp/audio", "ftp://example.com/file", "https://user@example.com/file", "not a url"} {
		if _, err := validateUploadURL(raw); err == nil {
			t.Fatalf("unsafe URL accepted: %q", raw)
		}
	}
	if _, err := validateUploadURL("https://example.com/file?signature=1"); err != nil {
		t.Fatal(err)
	}
}
