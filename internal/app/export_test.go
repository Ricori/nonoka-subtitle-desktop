package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestNormalizeSubtitleFilename(t *testing.T) {
	tests := map[string]string{
		" episode 01 ": "episode 01.ass",
		"clip.ass":     "clip.ass",
		"bad:name.ass": "bad_name.ass",
		"":             "subtitle.ass",
	}
	for input, want := range tests {
		if got := normalizeSubtitleFilename(input); got != want {
			t.Errorf("normalizeSubtitleFilename(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeExportFilename(t *testing.T) {
	if got := normalizeExportFilename(`bad:name?.mov`); got != "bad_name_.mov.mp4" {
		t.Fatalf("unexpected filename: %q", got)
	}
	if got := normalizeExportFilename(" clip.MP4 "); got != "clip.MP4" {
		t.Fatalf("existing extension changed: %q", got)
	}
}

func TestValidateExportOptions(t *testing.T) {
	out := filepath.Join(t.TempDir(), "result.mp4")
	spec, err := validateExportOptions(ExportOptions{ID: "video_1", T0: -2, T1: 5, OutPath: out})
	if err != nil {
		t.Fatal(err)
	}
	if spec.T0 != 0 || spec.Duration != 5 || spec.CRF != 21 || spec.Preset != "medium" || spec.ABR != "192k" {
		t.Fatalf("unexpected defaults: %+v", spec)
	}
	if _, err := validateExportOptions(ExportOptions{ID: "video_1", T0: 5, T1: 2, OutPath: out}); err == nil {
		t.Fatal("invalid range accepted")
	}
	if _, err := validateExportOptions(ExportOptions{ID: "video_1", T1: 2, OutPath: "relative.mp4"}); err == nil {
		t.Fatal("relative output accepted")
	}
}

func TestBuildExportArgs(t *testing.T) {
	spec := exportSpec{ExportOptions: ExportOptions{
		T0: 1.25, CRF: 19, Preset: "fast", ScaleH: 720, ABR: "256k",
	}, Duration: 3.5}
	args := buildExportArgs(`D:\source.mp4`, `D:\result.mp4.part`, spec)
	wantFilter := "scale=-2:720,subtitles=sub.ass:fontsdir=fonts"
	if !slices.Contains(args, wantFilter) {
		t.Fatalf("filter missing from %q", args)
	}
	joined := strings.Join(args, " ")
	for _, value := range []string{"-ss 1.25", "-t 3.5", "-preset fast", "-crf 19", "-b:a 256k", "-f mp4"} {
		if !strings.Contains(joined, value) {
			t.Fatalf("%q missing from %q", value, joined)
		}
	}
}

func TestPrepareExportWorkDir(t *testing.T) {
	previousAssets := bundledAssets
	bundledAssets = fstest.MapFS{
		"frontend/dist/fonts/first.woff2":  {Data: []byte("first")},
		"frontend/dist/fonts/second.woff2": {Data: []byte("second")},
	}
	t.Cleanup(func() { bundledAssets = previousAssets })
	dir, err := prepareExportWorkDir(t.TempDir(), "video_1", "[Script Info]\n")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	data, err := os.ReadFile(filepath.Join(dir, "sub.ass"))
	if err != nil || string(data) != "[Script Info]\n" {
		t.Fatalf("subtitle copy failed: %q, %v", data, err)
	}
	fonts, err := os.ReadDir(filepath.Join(dir, "fonts"))
	if err != nil || len(fonts) < 2 {
		t.Fatalf("font copy failed: %d, %v", len(fonts), err)
	}
}

func TestReplaceExportOutput(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "result.mp4")
	source := destination + ".part"
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceExportOutput(source, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "new" {
		t.Fatalf("replacement failed: %q, %v", data, err)
	}
	backups, err := filepath.Glob(destination + ".nonoka-backup-*")
	if err != nil || len(backups) != 0 {
		t.Fatalf("backup was not cleaned: %v, %v", backups, err)
	}
}

func TestReplacingJobKeepsNewCancellation(t *testing.T) {
	engine := &MediaEngine{jobs: make(map[string]*mediaJob)}
	first, finishFirst := engine.beginJob(context.Background(), "same")
	second, finishSecond := engine.beginJob(context.Background(), "same")
	defer finishSecond()
	select {
	case <-first.Done():
	case <-time.After(time.Second):
		t.Fatal("previous job was not canceled")
	}
	finishFirst()
	if !engine.Cancel("same") {
		t.Fatal("old cleanup removed the new job")
	}
	select {
	case <-second.Done():
	case <-time.After(time.Second):
		t.Fatal("new job was not canceled")
	}
}
