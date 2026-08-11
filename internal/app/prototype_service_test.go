package app

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMediaHandlerSupportsRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.mp4")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}

	service := newPrototypeService(time.Now())
	server, err := newLoopbackMediaServer(service.currentMediaPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	service.attachMediaServer(server)
	info, err := service.loadVideo(path)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, info.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=2-5")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", res.StatusCode)
	}
	if string(body) != "2345" {
		t.Fatalf("body = %q, want %q", body, "2345")
	}
	if got := res.Header.Get("Content-Range"); got != "bytes 2-5/10" {
		t.Fatalf("Content-Range = %q", got)
	}
}

func TestMediaServerIsLoopbackAndProtected(t *testing.T) {
	service := newPrototypeService(time.Now())
	server, err := newLoopbackMediaServer(service.currentMediaPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	parsed, err := url.Parse(server.URL(1))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Hostname() != "127.0.0.1" {
		t.Fatalf("hostname = %q", parsed.Hostname())
	}
	res, err := http.Get("http://" + parsed.Host + "/media/invalid")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", res.StatusCode)
	}
}

func TestMediaServerSupportsHeadAndRejectsPost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.mp4")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := newPrototypeService(time.Now())
	server, err := newLoopbackMediaServer(service.currentMediaPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	service.attachMediaServer(server)
	info, err := service.loadVideo(path)
	if err != nil {
		t.Fatal(err)
	}

	head, err := http.Head(info.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer head.Body.Close()
	if head.StatusCode != http.StatusOK || head.ContentLength != 10 {
		t.Fatalf("HEAD status=%d length=%d", head.StatusCode, head.ContentLength)
	}

	req, err := http.NewRequest(http.MethodPost, info.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", res.StatusCode)
	}
}

func TestMediaURLsStayBoundToTheirOriginalFiles(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	if err := os.WriteFile(first, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := newPrototypeService(time.Now())
	server, err := newLoopbackMediaServer(service.currentMediaPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	service.attachMediaServer(server)
	firstInfo, err := service.loadVideo(first)
	if err != nil {
		t.Fatal(err)
	}
	secondInfo, err := service.loadVideo(second)
	if err != nil {
		t.Fatal(err)
	}
	for url, want := range map[string]string{firstInfo.URL: "first", secondInfo.URL: "second"} {
		response, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil || string(body) != want {
			t.Fatalf("url %s body=%q err=%v, want %q", url, body, readErr, want)
		}
	}
}

func TestVideoInfoRejectsUnsupportedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.txt")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := videoInfo(path); err == nil {
		t.Fatal("videoInfo accepted an unsupported extension")
	}
}
