package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAppDataPersistsConfigAndLibrary(t *testing.T) {
	paths := appPathsAt(t.TempDir())
	manager := newFFmpegManagerWithOptions(ffmpegOptions{Path: filepath.Join(paths.FFmpegDir, "ffmpeg.exe")})
	service, err := newDesktopService(paths, manager, newPrototypeService(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	config := service.GetConfig()
	if config.Backend != backendBase || config.Stream != defaultStreamBase || config.Version != desktopVersion {
		t.Fatalf("public config = %#v", config)
	}
	config.CacheLimitGB = 7.5
	if _, err := service.SetConfig(config); err != nil {
		t.Fatal(err)
	}
	service.library = []libraryDiskEntry{{LibraryEntry: LibraryEntry{ID: "loc_test", Title: "sample.mp4"}}}
	if err := writeJSONAtomic(paths.LibraryFile, service.library); err != nil {
		t.Fatal(err)
	}

	reloaded, err := newDesktopService(paths, manager, newPrototypeService(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.GetConfig().CacheLimitGB != 7.5 || len(reloaded.GetLibrary()) != 1 {
		t.Fatalf("config = %#v, library = %#v", reloaded.GetConfig(), reloaded.GetLibrary())
	}
	var persisted map[string]json.RawMessage
	if err := readJSON(paths.ConfigFile, &persisted); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"backend", "stream", "version"} {
		if _, exists := persisted[key]; exists {
			t.Fatalf("read-only config field %q was persisted", key)
		}
	}
}

func TestOpenLibraryVideoAllowsCloudOnlyEntry(t *testing.T) {
	paths := appPathsAt(t.TempDir())
	manager := newFFmpegManagerWithOptions(ffmpegOptions{Path: filepath.Join(paths.FFmpegDir, "ffmpeg.exe")})
	prototype := newPrototypeService(time.Now())
	service, err := newDesktopService(paths, manager, prototype)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.OpenLibraryVideo("pPbTHcdy3UR"); err != nil {
		t.Fatalf("cloud-only video should open: %v", err)
	}
	if service.openInEditor != "pPbTHcdy3UR" || !prototype.formal || prototype.editorID != "pPbTHcdy3UR" {
		t.Fatalf("editor state = %q, formal=%v, id=%q", service.openInEditor, prototype.formal, prototype.editorID)
	}
	if _, err := service.OpenLibraryVideo("../escape"); err == nil {
		t.Fatal("unsafe media ID was accepted")
	}
}

func TestFingerprintMatchesElectronAlgorithm(t *testing.T) {
	data := []byte("nonoka fingerprint fixture")
	path := filepath.Join(t.TempDir(), "fixture.mp4")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	actual, err := fileFingerprint(path, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	expectedHash := sha256.New()
	_, _ = expectedHash.Write([]byte("26"))
	_, _ = expectedHash.Write(data)
	_, _ = expectedHash.Write(data)
	expected := hex.EncodeToString(expectedHash.Sum(nil))
	if actual != expected {
		t.Fatalf("fingerprint = %s, want %s", actual, expected)
	}
}

func TestParseMediaMetadata(t *testing.T) {
	text := `Input #0, mov,mp4, from 'sample.mp4':
  Duration: 00:02:03.456, start: 0.000000, bitrate: 1000 kb/s
  Stream #0:0: Video: h264, yuv420p, 1920x1080, 30 fps
  Stream #0:1: Audio: aac, 48000 Hz, stereo`
	metadata, err := parseMediaMetadata(text)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Duration != 123.456 || metadata.Width != 1920 || metadata.Height != 1080 || !metadata.HasVideo || !metadata.HasAudio {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestExposedCacheMethodsRejectUnsafeID(t *testing.T) {
	paths := appPathsAt(t.TempDir())
	manager := newFFmpegManagerWithOptions(ffmpegOptions{Path: filepath.Join(paths.FFmpegDir, "ffmpeg.exe")})
	service, err := newDesktopService(paths, manager, newPrototypeService(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if service.HasCache("../escape") || service.TouchCache("../escape") {
		t.Fatal("unsafe ID was accepted")
	}
	if _, err := service.CopyIntoCache("../escape", "unused"); err == nil {
		t.Fatal("unsafe copy ID was accepted")
	}
	if err := service.ClearCache("../escape"); err == nil {
		t.Fatal("unsafe clear ID was accepted")
	}
	if ok, err := service.RenameLibraryTitle("../escape", "title"); err == nil || ok {
		t.Fatal("unsafe rename ID was accepted")
	}
	if ok, err := service.SetClips("../escape", nil); err == nil || ok {
		t.Fatal("unsafe clips ID was accepted")
	}
	if ok, err := service.RenameLibraryID("../escape", "safe"); err == nil || ok {
		t.Fatal("unsafe primary-key rename was accepted")
	}
}

func TestElectronLibraryFieldsSurviveRoundTrip(t *testing.T) {
	fixture := `[{"id":"pPbTHcdy3UR","srcPath":"D:\\video.mp4","title":"video.mp4","size":42,"duration":3.5,"width":1920,"height":1080,"fp":"abc","addedAt":10,"lastAccess":20,"clips":[{"id":"clip1","name":"切片 1","t0":1.2,"t1":2.3,"createdAt":30}],"serverState":{"revision":7}}]`
	var entries []libraryDiskEntry
	if err := json.Unmarshal([]byte(fixture), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || len(entries[0].Clips) != 1 {
		t.Fatalf("entries = %#v", entries)
	}
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip []map[string]any
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatal(err)
	}
	serverState, ok := roundTrip[0]["serverState"].(map[string]any)
	if !ok || serverState["revision"] != float64(7) {
		t.Fatalf("serverState was lost: %s", data)
	}
	clips := roundTrip[0]["clips"].([]any)
	if clips[0].(map[string]any)["name"] != "切片 1" {
		t.Fatalf("Electron clip was lost: %s", data)
	}
}

func TestLibraryManagementPreservesExtrasAndRenamesAssets(t *testing.T) {
	paths := appPathsAt(t.TempDir())
	manager := newFFmpegManagerWithOptions(ffmpegOptions{Path: filepath.Join(paths.FFmpegDir, "ffmpeg.exe")})
	service, err := newDesktopService(paths, manager, newPrototypeService(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	service.library = []libraryDiskEntry{{
		LibraryEntry: LibraryEntry{ID: "loc_old", Title: "old", Clips: []Clip{}},
		Extra:        map[string]json.RawMessage{"serverState": json.RawMessage(`{"revision":7}`)},
	}}
	if err := service.saveLibraryLocked(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(service.cachePath("loc_old"), []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(service.thumbPath("loc_old"), []byte("thumb"), 0o600); err != nil {
		t.Fatal(err)
	}

	if ok, err := service.RenameLibraryTitle("loc_old", "renamed"); err != nil || !ok {
		t.Fatalf("rename title: ok=%v err=%v", ok, err)
	}
	clips := []Clip{
		{ID: strings.Repeat("a", 40), Name: strings.Repeat("字", 90), T0: -2, T1: 3},
		{ID: "invalid", Name: "invalid", T0: 4, T1: 4},
	}
	if ok, err := service.SetClips("loc_old", clips); err != nil || !ok {
		t.Fatalf("set clips: ok=%v err=%v", ok, err)
	}
	if ok, err := service.RenameLibraryID("loc_old", "server_id"); err != nil || !ok {
		t.Fatalf("rename id: ok=%v err=%v", ok, err)
	}
	if fileExists(service.cachePath("loc_old")) || !fileExists(service.cachePath("server_id")) {
		t.Fatal("cache file was not renamed")
	}
	if fileExists(service.thumbPath("loc_old")) || !fileExists(service.thumbPath("server_id")) {
		t.Fatal("thumbnail was not renamed")
	}

	var stored []libraryDiskEntry
	if err := readJSON(paths.LibraryFile, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].ID != "server_id" || stored[0].Title != "renamed" {
		t.Fatalf("stored entry = %#v", stored)
	}
	if len(stored[0].Clips) != 1 || stored[0].Clips[0].T0 != 0 || len([]rune(stored[0].Clips[0].ID)) != 32 || len([]rune(stored[0].Clips[0].Name)) != 80 {
		t.Fatalf("normalized clips = %#v", stored[0].Clips)
	}
	var serverState struct {
		Revision int `json:"revision"`
	}
	if err := json.Unmarshal(stored[0].Extra["serverState"], &serverState); err != nil || serverState.Revision != 7 {
		t.Fatalf("extra fields changed: %s", stored[0].Extra["serverState"])
	}
}

func TestRemoveLibraryDataHonoursOptions(t *testing.T) {
	paths := appPathsAt(t.TempDir())
	manager := newFFmpegManagerWithOptions(ffmpegOptions{Path: filepath.Join(paths.FFmpegDir, "ffmpeg.exe")})
	service, err := newDesktopService(paths, manager, newPrototypeService(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	service.library = []libraryDiskEntry{{LibraryEntry: LibraryEntry{ID: "loc_keep", Title: "keep"}}}
	if err := service.saveLibraryLocked(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(service.cachePath("loc_keep"), []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(service.thumbPath("loc_keep"), []byte("thumb"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.RemoveLibraryData("loc_keep", LibraryRemoveOptions{Cache: true}); err != nil {
		t.Fatal(err)
	}
	if service.HasCache("loc_keep") || len(service.GetLibrary()) != 1 || !fileExists(service.thumbPath("loc_keep")) {
		t.Fatal("cache-only removal removed other data")
	}
	if err := service.RemoveLibraryData("loc_keep", LibraryRemoveOptions{Thumb: true, Entry: true}); err != nil {
		t.Fatal(err)
	}
	if len(service.GetLibrary()) != 0 || fileExists(service.thumbPath("loc_keep")) {
		t.Fatal("entry removal did not finish")
	}
}

func TestMigrateCacheDirectoryMovesAvailableFiles(t *testing.T) {
	paths := appPathsAt(t.TempDir())
	manager := newFFmpegManagerWithOptions(ffmpegOptions{Path: filepath.Join(paths.FFmpegDir, "ffmpeg.exe")})
	service, err := newDesktopService(paths, manager, newPrototypeService(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"one.mp4": "one", "two.mp4": "two", "pending.part": "partial"} {
		if err := os.WriteFile(filepath.Join(paths.CacheDir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	destination := filepath.Join(t.TempDir(), "new-cache")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "two.mp4"), []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := service.MigrateCacheDirectory(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Moved != 1 || result.Kept != 1 {
		t.Fatalf("migration = %#v", result)
	}
	if !fileExists(filepath.Join(destination, "one.mp4")) || !fileExists(filepath.Join(paths.CacheDir, "two.mp4")) || !fileExists(filepath.Join(paths.CacheDir, "pending.part")) {
		t.Fatal("migration moved the wrong files")
	}
	if service.GetConfig().CacheDir != destination || !service.PathExists(destination) {
		t.Fatalf("config = %#v", service.GetConfig())
	}
}

func TestCacheThumbFromCloudPersistsJPEGOnce(t *testing.T) {
	jpeg := []byte{0xff, 0xd8, 0xff, 0xdb, 0x00, 0x01, 0xff, 0xd9}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.URL.Path != "/thumbs/cloud_id.jpg" {
			http.NotFound(response, request)
			return
		}
		_, _ = response.Write(jpeg)
	}))
	defer server.Close()

	paths := appPathsAt(t.TempDir())
	service, err := newDesktopService(paths, newFFmpegManagerWithOptions(ffmpegOptions{}), newPrototypeService(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	service.streamBase = server.URL
	service.httpClient = server.Client()
	sourceURL := server.URL + "/thumbs/cloud_id.jpg"
	if !service.CacheThumbFromCloud("cloud_id", sourceURL) {
		t.Fatal("cloud thumbnail was not cached")
	}
	stored, err := os.ReadFile(service.thumbPath("cloud_id"))
	if err != nil || string(stored) != string(jpeg) {
		t.Fatalf("stored thumbnail = %x, err = %v", stored, err)
	}
	if service.CacheThumbFromCloud("cloud_id", sourceURL) || requests.Load() != 1 {
		t.Fatalf("existing thumbnail fetched again: requests=%d", requests.Load())
	}
	if service.CacheThumbFromCloud("../escape", sourceURL) || service.CacheThumbFromCloud("other", sourceURL) {
		t.Fatal("invalid thumbnail request was accepted")
	}
	if fileExists(service.thumbPath("cloud_id") + ".part") {
		t.Fatal("thumbnail cache left a partial file")
	}
}

func TestSetClipsMatchesElectronLimit(t *testing.T) {
	paths := appPathsAt(t.TempDir())
	manager := newFFmpegManagerWithOptions(ffmpegOptions{Path: filepath.Join(paths.FFmpegDir, "ffmpeg.exe")})
	service, err := newDesktopService(paths, manager, newPrototypeService(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	service.library = []libraryDiskEntry{{LibraryEntry: LibraryEntry{ID: "loc_clips"}}}
	clips := make([]Clip, 205)
	for index := range clips {
		clips[index] = Clip{ID: "clip", Name: "clip", T0: float64(index), T1: float64(index + 1)}
	}
	if ok, err := service.SetClips("loc_clips", clips); err != nil || !ok {
		t.Fatalf("set clips: ok=%v err=%v", ok, err)
	}
	if got := len(service.GetClips("loc_clips")); got != clipMax {
		t.Fatalf("clips = %d, want %d", got, clipMax)
	}
}

func TestSetClipsCreatesLocalEntryForRemoteVideo(t *testing.T) {
	paths := appPathsAt(t.TempDir())
	manager := newFFmpegManagerWithOptions(ffmpegOptions{Path: filepath.Join(paths.FFmpegDir, "ffmpeg.exe")})
	service, err := newDesktopService(paths, manager, newPrototypeService(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	clips := []Clip{{ID: "clip", Name: "remote clip", T0: 1, T1: 2}}
	if ok, err := service.SetClips("remote_video", clips); err != nil || !ok {
		t.Fatalf("set clips: ok=%v err=%v", ok, err)
	}

	reloaded, err := newDesktopService(paths, manager, newPrototypeService(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.GetClips("remote_video"); len(got) != 1 || got[0].Name != "remote clip" {
		t.Fatalf("reloaded clips = %#v", got)
	}
}

func TestSyncCloudLibraryPersistsRemoteEntries(t *testing.T) {
	paths := appPathsAt(t.TempDir())
	manager := newFFmpegManagerWithOptions(ffmpegOptions{Path: filepath.Join(paths.FFmpegDir, "ffmpeg.exe")})
	service, err := newDesktopService(paths, manager, newPrototypeService(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	service.library = []libraryDiskEntry{{LibraryEntry: LibraryEntry{
		ID: "local", SrcPath: `D:\video.mp4`, Title: "本地标题", Fingerprint: "same",
	}}}

	entries, err := service.SyncCloudLibrary([]CloudLibraryEntry{
		{VideoID: "cloud", Title: "云端视频", Fingerprint: "remote-fp", CreatedAt: 123.5},
		{VideoID: "local", Title: "云端标题", Fingerprint: "same", CreatedAt: 99},
		{VideoID: "../invalid", Title: "invalid"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entry count = %d, want 2", len(entries))
	}
	if entries[0].Title != "本地标题" || entries[0].CloudOnly {
		t.Fatalf("local entry was overwritten: %#v", entries[0])
	}
	if entries[1].ID != "cloud" || entries[1].Title != "云端视频" || entries[1].Fingerprint != "remote-fp" || entries[1].AddedAt != 123500 || !entries[1].CloudOnly {
		t.Fatalf("cloud entry = %#v", entries[1])
	}
	entries, err = service.SyncCloudLibrary([]CloudLibraryEntry{{
		VideoID: "cloud", Title: "云端新标题", Fingerprint: "remote-fp", CreatedAt: 123.5,
	}})
	if err != nil || len(entries) != 2 || entries[1].Title != "云端新标题" {
		t.Fatalf("second sync: entries=%#v err=%v", entries, err)
	}

	reloaded, err := newDesktopService(paths, manager, newPrototypeService(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	stored := reloaded.GetLibrary()
	if len(stored) != 2 || stored[1].ID != "cloud" || !stored[1].CloudOnly {
		t.Fatalf("reloaded library = %#v", stored)
	}
	stored, err = reloaded.SyncCloudLibrary(nil)
	if err != nil || len(stored) != 1 || stored[0].ID != "local" {
		t.Fatalf("pruned library: entries=%#v err=%v", stored, err)
	}
}
