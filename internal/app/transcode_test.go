package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildTranscodeArgs(t *testing.T) {
	args := strings.Join(buildTranscodeArgs(`D:\hevc.mkv`, `D:\cache\v1.mp4.h264.part`), " ")
	for _, value := range []string{
		"-c:v libx264", "-pix_fmt yuv420p", "-c:a aac", "-movflags +faststart",
		"-f mp4", "-progress pipe:1", `D:\cache\v1.mp4.h264.part`,
	} {
		if !strings.Contains(args, value) {
			t.Fatalf("%q missing from %q", value, args)
		}
	}
}

func TestTranscodeSourcePrefersOriginalOverCache(t *testing.T) {
	service := newResolutionTestService(t)
	id := "tc_source"
	source := filepath.Join(t.TempDir(), "original.mkv")
	if err := os.WriteFile(source, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(service.cachePath(id), []byte("cached"), 0o600); err != nil {
		t.Fatal(err)
	}
	service.library = []libraryDiskEntry{{LibraryEntry: LibraryEntry{ID: id, SrcPath: source}}}
	if path, err := service.transcodeSource(id); err != nil || path != source {
		t.Fatalf("path=%q err=%v", path, err)
	}

	// 原文件没了就只能拿缓存副本转，转完覆盖回同一个位置
	service.library = []libraryDiskEntry{{LibraryEntry: LibraryEntry{ID: id, SrcPath: filepath.Join(t.TempDir(), "gone.mkv")}}}
	if path, err := service.transcodeSource(id); err != nil || path != service.cachePath(id) {
		t.Fatalf("path=%q err=%v", path, err)
	}
}

func TestTranscodeSourceReportsMissingFile(t *testing.T) {
	service := newResolutionTestService(t)
	if _, err := service.transcodeSource("tc_missing"); err == nil {
		t.Fatal("expected an error when no local file is left")
	}
}

func TestInstallTranscodedReplacesCacheCopy(t *testing.T) {
	service := newResolutionTestService(t)
	id := "tc_install"
	destination := service.cachePath(id)
	if err := os.WriteFile(destination, []byte("hevc"), 0o600); err != nil {
		t.Fatal(err)
	}
	part := destination + ".h264.part"
	if err := os.WriteFile(part, []byte("h264"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.installTranscoded(id, part, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "h264" {
		t.Fatalf("cache copy = %q, %v", data, err)
	}
	if fileExists(part) {
		t.Fatal("临时文件没清掉")
	}
}

func TestTranscodeRejectsInvalidID(t *testing.T) {
	service := newResolutionTestService(t)
	if _, err := service.TranscodeToH264("../etc"); err == nil {
		t.Fatal("expected an error for an invalid media ID")
	}
}
