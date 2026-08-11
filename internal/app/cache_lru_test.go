package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newCacheTestService(t *testing.T) *DesktopService {
	t.Helper()
	service, err := newDesktopService(
		appPathsAt(t.TempDir()),
		newFFmpegManagerWithOptions(ffmpegOptions{}),
		newPrototypeService(time.Now()),
	)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func writeCacheFixture(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSweepPartFilesOnlyRemovesPartialDownloads(t *testing.T) {
	service := newCacheTestService(t)
	part := filepath.Join(service.config.CacheDir, "pending.mp4.part")
	video := filepath.Join(service.config.CacheDir, "kept.mp4")
	writeCacheFixture(t, part, 7)
	writeCacheFixture(t, video, 11)

	if removed := service.sweepPartFiles(); removed != 7 {
		t.Fatalf("removed bytes = %d, want 7", removed)
	}
	if fileExists(part) || !fileExists(video) {
		t.Fatal("partial cleanup removed the wrong file")
	}
}

func TestConvergeCacheUsesLRUAndKeepsOpenEditor(t *testing.T) {
	service := newCacheTestService(t)
	service.config.CacheLimitGB = float64(12) / (1024 * 1024 * 1024)
	service.library = []libraryDiskEntry{
		{LibraryEntry: LibraryEntry{ID: "old", LastAccess: 10}},
		{LibraryEntry: LibraryEntry{ID: "open", LastAccess: 1}},
		{LibraryEntry: LibraryEntry{ID: "new", LastAccess: 30}},
	}
	writeCacheFixture(t, service.cachePath("old"), 8)
	writeCacheFixture(t, service.cachePath("open"), 8)
	writeCacheFixture(t, service.cachePath("new"), 8)
	if !service.SetOpenInEditor("open") {
		t.Fatal("failed to mark the editor video")
	}

	service.cacheMu.Lock()
	err := service.convergeCacheLocked()
	service.cacheMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if fileExists(service.cachePath("old")) {
		t.Fatal("oldest removable cache was retained")
	}
	if !fileExists(service.cachePath("open")) {
		t.Fatal("open editor cache was removed")
	}
}

func TestConvergeCacheIncludesUnknownVideoFiles(t *testing.T) {
	service := newCacheTestService(t)
	service.config.CacheLimitGB = float64(8) / (1024 * 1024 * 1024)
	unknown := filepath.Join(service.config.CacheDir, "remote-only.mp4")
	known := service.cachePath("known")
	writeCacheFixture(t, unknown, 8)
	writeCacheFixture(t, known, 8)
	oldTime := time.Unix(10, 0)
	if err := os.Chtimes(unknown, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	service.library = []libraryDiskEntry{{LibraryEntry: LibraryEntry{ID: "known", LastAccess: 30_000}}}

	service.cacheMu.Lock()
	err := service.convergeCacheLocked()
	service.cacheMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if fileExists(unknown) || !fileExists(known) {
		t.Fatal("unknown cache file did not participate in LRU cleanup")
	}
}
