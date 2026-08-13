package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestClampSpectrogramRange(t *testing.T) {
	start, duration, err := clampSpectrogramRange(9.9996, 5, 12)
	if err != nil {
		t.Fatal(err)
	}
	if start != 10 || duration != 2 {
		t.Fatalf("unexpected range: start=%v duration=%v", start, duration)
	}
}

func TestClampSpectrogramRangeRejectsPastEnd(t *testing.T) {
	if _, _, err := clampSpectrogramRange(12, 5, 12); err == nil {
		t.Fatal("expected range error")
	}
}

func TestTrimSpectrogramCacheKeepsRecentTiles(t *testing.T) {
	dir := t.TempDir()
	paths := make([]string, 4)
	for i := range paths {
		paths[i] = filepath.Join(dir, string(rune('a'+i))+".png")
		if err := os.WriteFile(paths[i], []byte("tile"), 0o644); err != nil {
			t.Fatal(err)
		}
		stamp := time.Unix(int64(i+1), 0)
		if err := os.Chtimes(paths[i], stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	trimSpectrogramCache(dir, paths[0], 2)
	if !fileExists(paths[0]) || !fileExists(paths[3]) {
		t.Fatal("expected kept and newest tiles to survive")
	}
	if fileExists(paths[1]) || fileExists(paths[2]) {
		t.Fatal("expected oldest removable tiles to be pruned")
	}
}

func TestSpectrogramDurationIsAnInputOption(t *testing.T) {
	args := spectrogramArgs("in.mp4", "out.png", 2.5, 5)
	tIndex, inputIndex := slices.Index(args, "-t"), slices.Index(args, "-i")
	if tIndex < 0 || inputIndex < 0 || tIndex > inputIndex {
		t.Fatalf("-t must precede -i so showspectrumpic only reads the requested tile: %v", args)
	}
}
