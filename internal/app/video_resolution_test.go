package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestResolveVideoPrefersCacheAndServesRanges(t *testing.T) {
	service := newResolutionTestService(t)
	id := "cached_video"
	payload := []byte("0123456789")
	if err := os.WriteFile(service.cachePath(id), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	resolution, err := service.ResolveVideo(id, VideoResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.State != "cached" || resolution.URL == "" {
		t.Fatalf("resolution = %#v", resolution)
	}
	request, _ := http.NewRequest(http.MethodGet, resolution.URL, nil)
	request.Header.Set("Range", "bytes=2-5")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusPartialContent || string(body) != "2345" {
		t.Fatalf("range status=%d body=%q", response.StatusCode, body)
	}
}

func TestResolveVideoUsesSourceAndCopiesInBackground(t *testing.T) {
	service := newResolutionTestService(t)
	id := "source_video"
	source := filepath.Join(t.TempDir(), "source.mp4")
	payload := bytes.Repeat([]byte("video"), 4096)
	if err := os.WriteFile(source, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	service.library = []libraryDiskEntry{{LibraryEntry: LibraryEntry{ID: id, SrcPath: source}}}
	resolution, err := service.ResolveVideo(id, VideoResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.State != "source" || resolution.URL == "" {
		t.Fatalf("resolution = %#v", resolution)
	}
	waitFor(t, func() bool { return fileExists(service.cachePath(id)) })
	cached, _ := os.ReadFile(service.cachePath(id))
	if !bytes.Equal(cached, payload) {
		t.Fatal("background copy changed the video")
	}
}

func TestResolveVideoDownloadsFromR2(t *testing.T) {
	payload := bytes.Repeat([]byte("cloud-video"), 1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/uploads/cloud_video.mp4" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", "11264")
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	service := newResolutionTestService(t)
	service.streamBase = server.URL

	resolution, err := service.ResolveVideo("cloud_video", VideoResolveOptions{CanR2: true})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.State != "downloading" {
		t.Fatalf("resolution = %#v", resolution)
	}
	waitFor(t, func() bool { return fileExists(service.cachePath("cloud_video")) })
	cached, _ := os.ReadFile(service.cachePath("cloud_video"))
	if !bytes.Equal(cached, payload) {
		t.Fatal("downloaded cache differs from response")
	}
}

func TestDownloadRejectsTruncatedResponseAndRemovesPart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "20")
		_, _ = w.Write([]byte("short"))
	}))
	defer server.Close()
	service := newResolutionTestService(t)
	service.streamBase = server.URL
	if _, err := service.DownloadFromR2("truncated"); err == nil {
		t.Fatal("truncated response was accepted")
	}
	if fileExists(service.cachePath("truncated")) || fileExists(service.cachePath("truncated")+".part") {
		t.Fatal("truncated download left cache data")
	}
}

func TestCancelR2DownloadRemovesPartialFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "67108864")
		flusher, _ := w.(http.Flusher)
		chunk := bytes.Repeat([]byte("x"), 64*1024)
		for {
			select {
			case <-r.Context().Done():
				return
			default:
				if _, err := w.Write(chunk); err != nil {
					return
				}
				if flusher != nil {
					flusher.Flush()
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
	}))
	defer server.Close()
	service := newResolutionTestService(t)
	service.streamBase = server.URL
	if _, err := service.ResolveVideo("cancel_download", VideoResolveOptions{CanR2: true}); err != nil {
		t.Fatal(err)
	}
	part := service.cachePath("cancel_download") + ".part"
	waitFor(t, func() bool { return fileExists(part) })
	if !service.CancelMediaJob("cancel_download") {
		t.Fatal("download was not cancelled")
	}
	waitFor(t, func() bool {
		service.videoMu.Lock()
		defer service.videoMu.Unlock()
		return service.videoJobs["cancel_download"] == nil
	})
	if fileExists(part) || fileExists(service.cachePath("cancel_download")) {
		t.Fatal("cancelled download left cache data")
	}
}

func TestBackgroundVideoJobDeduplicatesAndCancels(t *testing.T) {
	service := newResolutionTestService(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	task := func(ctx context.Context) (string, error) {
		calls.Add(1)
		close(started)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-release:
			return "", nil
		}
	}
	if !service.startVideoBackground("dedupe", task) {
		t.Fatal("first job did not start")
	}
	<-started
	if service.startVideoBackground("dedupe", task) {
		t.Fatal("duplicate job started")
	}
	if !service.CancelMediaJob("dedupe") {
		t.Fatal("job was not cancelled")
	}
	waitFor(t, func() bool {
		service.videoMu.Lock()
		defer service.videoMu.Unlock()
		return service.videoJobs["dedupe"] == nil
	})
	if calls.Load() != 1 {
		t.Fatalf("task calls = %d", calls.Load())
	}
}

func TestValidateVideoSelectionMatchesElectronFingerprint(t *testing.T) {
	service := newResolutionTestService(t)
	path := filepath.Join(t.TempDir(), "selected.mp4")
	data := []byte("selected video")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := fileFingerprint(path, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	selection, err := service.validateVideoSelection(path, VideoExpectation{Fingerprint: fingerprint})
	if err != nil || !selection.OK || selection.Path != path {
		t.Fatalf("selection = %#v, err = %v", selection, err)
	}
	mismatch, err := service.validateVideoSelection(path, VideoExpectation{Fingerprint: "different"})
	if err != nil || mismatch.OK || mismatch.Warn == "" || mismatch.Path != path {
		t.Fatalf("mismatch = %#v, err = %v", mismatch, err)
	}
}

func TestAttachLocalVideoPreservesElectronFields(t *testing.T) {
	service := newResolutionTestService(t)
	id := "attached"
	source := filepath.Join(t.TempDir(), "attached.mp4")
	if err := os.WriteFile(source, []byte("new source"), 0o600); err != nil {
		t.Fatal(err)
	}
	service.library = []libraryDiskEntry{{
		LibraryEntry: LibraryEntry{ID: id, Title: "server title", Duration: 42},
		Extra:        map[string]json.RawMessage{"serverState": json.RawMessage(`{"revision":9}`)},
	}}
	result, err := service.AttachLocalVideo(id, source)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.URL == "" {
		t.Fatalf("result = %#v", result)
	}
	waitFor(t, func() bool { return fileExists(service.cachePath(id)) })
	var stored []libraryDiskEntry
	if err := readJSON(service.paths.LibraryFile, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 {
		t.Fatalf("stored entry count = %d", len(stored))
	}
	var serverState struct {
		Revision int `json:"revision"`
	}
	stateErr := json.Unmarshal(stored[0].Extra["serverState"], &serverState)
	if stored[0].SrcPath != source || stored[0].Duration != 42 || stateErr != nil || serverState.Revision != 9 {
		t.Fatalf("stored entry = %#v", stored)
	}
}

func TestResolveVideoReportsMissingReason(t *testing.T) {
	service := newResolutionTestService(t)
	service.library = []libraryDiskEntry{{LibraryEntry: LibraryEntry{ID: "missing", SrcPath: filepath.Join(t.TempDir(), "gone.mp4")}}}
	resolution, err := service.ResolveVideo("missing", VideoResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.State != "missing" || resolution.Reason == "" {
		t.Fatalf("resolution = %#v", resolution)
	}
}

func newResolutionTestService(t *testing.T) *DesktopService {
	t.Helper()
	paths := appPathsAt(t.TempDir())
	prototype := newPrototypeService(time.Now())
	server, err := newLoopbackMediaServer(prototype.currentMediaPath)
	if err != nil {
		t.Fatal(err)
	}
	prototype.attachMediaServer(server)
	t.Cleanup(func() { _ = server.Close() })
	manager := newFFmpegManagerWithOptions(ffmpegOptions{Path: filepath.Join(paths.FFmpegDir, "ffmpeg.exe")})
	service, err := newDesktopService(paths, manager, prototype)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}
