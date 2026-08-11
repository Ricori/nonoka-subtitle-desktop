package app

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestValidWindowBounds(t *testing.T) {
	if !validWindowBounds(WindowBounds{X: -1200, Y: 20, Width: 1180, Height: 760}, 960, 620) {
		t.Fatal("valid multi-monitor bounds were rejected")
	}
	if validWindowBounds(WindowBounds{Width: 400, Height: 300}, 960, 620) {
		t.Fatal("undersized bounds were accepted")
	}
}

func TestWindowBoundsMustIntersectAWorkArea(t *testing.T) {
	screens := []*application.Screen{{WorkArea: application.Rect{X: 0, Y: 0, Width: 1920, Height: 1040}}}
	if !windowBoundsOnScreens(WindowBounds{X: 1800, Y: 100, Width: 300, Height: 300}, screens) {
		t.Fatal("partially visible window was rejected")
	}
	if windowBoundsOnScreens(WindowBounds{X: 2500, Y: 100, Width: 300, Height: 300}, screens) {
		t.Fatal("off-screen window was accepted")
	}
}

func TestSavedNormalEditorBoundsOverrideDefaultMaximisedState(t *testing.T) {
	service := newCacheTestService(t)
	service.config.Bounds["editor"] = WindowBounds{X: 40, Y: 50, Width: 1200, Height: 800}
	options := application.WebviewWindowOptions{
		MinWidth: 1024, MinHeight: 640, StartState: application.WindowStateMaximised,
	}
	got := service.applyWindowOptions("editor", options)
	if got.StartState != application.WindowStateNormal || got.InitialPosition != application.WindowXY {
		t.Fatalf("window state = %v, position = %v", got.StartState, got.InitialPosition)
	}
}

func TestMaximisedWindowDefersStartStateUntilShown(t *testing.T) {
	options := application.WebviewWindowOptions{StartState: application.WindowStateMaximised}
	got, deferred := deferRestoredWindowStartState(options)
	if !got.Hidden || got.StartState != application.WindowStateNormal || deferred != application.WindowStateMaximised {
		t.Fatalf("window state = %v, deferred = %v", got.StartState, deferred)
	}
}

func TestNormalWindowKeepsVisibility(t *testing.T) {
	options := application.WebviewWindowOptions{StartState: application.WindowStateNormal}
	got, deferred := deferRestoredWindowStartState(options)
	if got.Hidden || got.StartState != application.WindowStateNormal || deferred != application.WindowStateNormal {
		t.Fatalf("window state = %v, deferred = %v", got.StartState, deferred)
	}
}

func TestPersistWindowBoundsKeepsElectronConfigShape(t *testing.T) {
	service := newCacheTestService(t)
	want := WindowBounds{X: 50, Y: 70, Width: 1200, Height: 800, Maximized: true}
	service.persistWindowBounds("home", want)

	reloaded := appConfigDisk{}
	if err := readJSON(service.paths.ConfigFile, &reloaded); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Bounds["home"]; got != want {
		t.Fatalf("saved bounds = %#v, want %#v", got, want)
	}
}
