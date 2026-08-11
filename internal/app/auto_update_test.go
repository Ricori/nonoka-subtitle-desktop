package app

import (
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

func TestNewerVersion(t *testing.T) {
	for _, test := range []struct {
		candidate string
		current   string
		want      bool
	}{
		{"0.2.0", "0.1.9", true},
		{"v1.0.1", "1.0.0", true},
		{"1.0.0", "1.0.0", false},
		{"0.9.9", "1.0.0", false},
	} {
		if got := newerVersion(test.candidate, test.current); got != test.want {
			t.Fatalf("newerVersion(%q, %q) = %v", test.candidate, test.current, got)
		}
	}
}

func TestRemoveReplacedExecutables(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "Nonoka Subtitle.exe")
	oldFiles := []string{
		executable + ".old.1786267404207847300",
		executable + ".old.1786267404207847301",
	}
	for _, path := range append([]string{executable, filepath.Join(directory, "keep.old.1")}, oldFiles...) {
		if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if remaining := removeReplacedExecutables(executable); remaining != 0 {
		t.Fatalf("%d replaced executables remain", remaining)
	}
	for _, path := range oldFiles {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("old executable was not removed: %s", path)
		}
	}
	for _, path := range []string{executable, filepath.Join(directory, "keep.old.1")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unrelated file was removed: %s", path)
		}
	}
}

func TestAutoUpdateDownloadsAndVerifiesEndpointArtifact(t *testing.T) {
	artifact := []byte("nonoka update fixture")
	digest := sha512.Sum512(artifact)
	parts := strings.Split(desktopVersion, ".")
	patch, _ := strconv.Atoi(parts[2])
	updateVersion := fmt.Sprintf("%s.%s.%d", parts[0], parts[1], patch+1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/latest.json":
			_ = json.NewEncoder(response).Encode(map[string]any{
				"schemaVersion": 1,
				"version":       updateVersion,
				"channel":       "stable",
				"notes":         "update smoke",
				"artifacts": []map[string]any{{
					"url": "/artifact.exe", "platform": runtime.GOOS, "arch": runtime.GOARCH,
					"filename": "Nonoka-Subtitle-windows-amd64.exe", "size": len(artifact),
					"digestAlgo": "sha512", "digest": base64.StdEncoding.EncodeToString(digest[:]),
				}},
			})
		case "/artifact.exe":
			_, _ = response.Write(artifact)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	t.Setenv("NONOKA_WAILS_UPDATE_MANIFEST", server.URL+"/latest.json")
	service := newCacheTestService(t)
	app := application.New(application.Options{Name: "update-test"})
	service.app = app
	if err := service.initAutoUpdate(app); err != nil {
		t.Fatal(err)
	}
	service.checkForUpdate(t.Context(), false)
	status := service.GetUpdateStatus()
	if !status.Ready || status.Version != updateVersion || status.Stage != "ready" {
		t.Fatalf("update status = %#v", status)
	}
	downloaded := app.Updater.DownloadedPath()
	if !fileExists(downloaded) {
		t.Fatal("verified update artifact was not staged")
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(downloaded)) })
	var notes ReleaseNotes
	if err := readJSON(service.releaseNotesPath(), &notes); err != nil || notes.Version != updateVersion {
		t.Fatalf("release notes = %#v, err = %v", notes, err)
	}
}

func TestReleaseMandatoryUsesManifestMetadata(t *testing.T) {
	release := &updater.Release{Metadata: map[string]any{"minVersion": "0.2.0"}}
	if !releaseMandatory(release, "0.1.0") || releaseMandatory(release, "0.2.0") {
		t.Fatal("mandatory version comparison is incorrect")
	}
}

func TestConsumeReleaseNotesOnce(t *testing.T) {
	service, err := newDesktopService(
		appPathsAt(t.TempDir()),
		newFFmpegManagerWithOptions(ffmpegOptions{}),
		newPrototypeService(time.Now()),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := ReleaseNotes{Version: desktopVersion, Notes: "fixed playback"}
	if err := writeJSONAtomic(service.releaseNotesPath(), want); err != nil {
		t.Fatal(err)
	}
	got, err := service.ConsumeReleaseNotes()
	if err != nil || got == nil || *got != want {
		t.Fatalf("notes = %#v, err = %v", got, err)
	}
	if _, err := os.Stat(service.releaseNotesPath()); !os.IsNotExist(err) {
		t.Fatal("release notes were not consumed")
	}
}
