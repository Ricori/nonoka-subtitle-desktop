package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const maxVideoDuration = 2 * 60 * 60

const (
	backendBase    = "https://ricori--ytapi.modal.run"
	desktopVersion = "0.6.6"
)

type AppConfig struct {
	TaskKey      string                  `json:"taskKey"`
	CacheDir     string                  `json:"cacheDir"`
	CacheLimitGB float64                 `json:"cacheLimitGB"`
	Bounds       map[string]WindowBounds `json:"bounds"`
	Backend      string                  `json:"backend"`
	Stream       string                  `json:"stream"`
	Version      string                  `json:"version"`
}

type appConfigDisk struct {
	TaskKey      string                  `json:"taskKey"`
	CacheDir     string                  `json:"cacheDir"`
	CacheLimitGB float64                 `json:"cacheLimitGB"`
	Bounds       map[string]WindowBounds `json:"bounds"`
}

type WindowBounds struct {
	X          int  `json:"x"`
	Y          int  `json:"y"`
	Width      int  `json:"width"`
	Height     int  `json:"height"`
	Maximized  bool `json:"maximized"`
	FullScreen bool `json:"fullScreen"`
}

type LibraryEntry struct {
	ID          string  `json:"id"`
	SrcPath     string  `json:"srcPath"`
	Title       string  `json:"title"`
	Size        int64   `json:"size"`
	Duration    float64 `json:"duration"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	Fingerprint string  `json:"fp"`
	AddedAt     int64   `json:"addedAt"`
	LastAccess  int64   `json:"lastAccess"`
	Clips       []Clip  `json:"clips,omitempty"`
	CloudOnly   bool    `json:"cloudOnly,omitempty"`
}

type CloudLibraryEntry struct {
	VideoID     string  `json:"video_id"`
	Title       string  `json:"title"`
	Fingerprint string  `json:"fp"`
	CreatedAt   float64 `json:"created_at"`
}

type Clip struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	T0        float64 `json:"t0"`
	T1        float64 `json:"t1"`
	CreatedAt int64   `json:"createdAt"`
}

type libraryDiskEntry struct {
	LibraryEntry
	Extra map[string]json.RawMessage `json:"-"`
}

type ImportFailure struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Error string `json:"error"`
}

type ImportResult struct {
	Added  []LibraryEntry  `json:"added"`
	Failed []ImportFailure `json:"failed"`
}

type CacheStats struct {
	Bytes      int64  `json:"bytes"`
	Files      int    `json:"files"`
	LimitBytes int64  `json:"limitBytes"`
	Dir        string `json:"dir"`
}

type LibraryRemoveOptions struct {
	Cache bool `json:"cache"`
	Thumb bool `json:"thumb"`
	Entry bool `json:"entry"`
}

type CacheMigrationResult struct {
	OK    bool   `json:"ok"`
	Dir   string `json:"dir"`
	Moved int    `json:"moved"`
	Kept  int    `json:"kept"`
}

type ThumbReady struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

type DesktopService struct {
	mu            sync.RWMutex
	cacheMu       sync.Mutex
	spectrogramMu sync.Mutex
	videoMu       sync.Mutex
	updateMu      sync.Mutex
	paths         AppPaths
	config        AppConfig
	library       []libraryDiskEntry
	ffmpeg        *FFmpegManager
	media         *MediaEngine
	prototype     *PrototypeService
	app           *application.App
	home          *application.WebviewWindow
	thumbJobs     map[string]chan struct{}
	videoJobs     map[string]context.CancelFunc
	openInEditor  string
	streamBase    string
	httpClient    *http.Client
	updateStatus  UpdateStatus
	updateBusy    bool
}

var validLibraryID = regexp.MustCompile(`^[0-9A-Za-z_-]{1,64}$`)

func (entry *libraryDiskEntry) UnmarshalJSON(data []byte) error {
	type plain LibraryEntry
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, key := range []string{"id", "srcPath", "title", "size", "duration", "width", "height", "fp", "addedAt", "lastAccess", "clips", "cloudOnly"} {
		delete(fields, key)
	}
	entry.LibraryEntry = LibraryEntry(decoded)
	entry.Extra = fields
	return nil
}

func (entry libraryDiskEntry) MarshalJSON() ([]byte, error) {
	type plain LibraryEntry
	data, err := json.Marshal(plain(entry.LibraryEntry))
	if err != nil {
		return nil, err
	}
	return mergeExtraJSON(data, entry.Extra)
}

func mergeExtraJSON(data []byte, extra map[string]json.RawMessage) ([]byte, error) {
	if len(extra) == 0 {
		return data, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for key, value := range extra {
		if _, exists := fields[key]; !exists {
			fields[key] = value
		}
	}
	return json.Marshal(fields)
}

func newDesktopService(paths AppPaths, ffmpeg *FFmpegManager, prototype *PrototypeService) (*DesktopService, error) {
	service := &DesktopService{
		paths: paths, ffmpeg: ffmpeg, prototype: prototype, thumbJobs: make(map[string]chan struct{}),
		videoJobs: make(map[string]context.CancelFunc), streamBase: defaultStreamBase, httpClient: &http.Client{},
		config: AppConfig{CacheDir: paths.CacheDir, CacheLimitGB: 20, Bounds: make(map[string]WindowBounds)},
	}
	if err := paths.ensure(); err != nil {
		return nil, err
	}
	if err := readJSON(paths.ConfigFile, &service.config); err != nil {
		return nil, fmt.Errorf("读取配置失败：%w", err)
	}
	if service.config.CacheDir == "" {
		service.config.CacheDir = paths.CacheDir
	}
	if service.config.CacheLimitGB <= 0 {
		service.config.CacheLimitGB = 20
	}
	if service.config.Bounds == nil {
		service.config.Bounds = make(map[string]WindowBounds)
	}
	service.applyPublicConfig(&service.config)
	if err := readJSON(paths.LibraryFile, &service.library); err != nil {
		return nil, fmt.Errorf("读取媒体库失败：%w", err)
	}
	if err := os.MkdirAll(service.config.CacheDir, 0o755); err != nil {
		return nil, err
	}
	return service, nil
}

func (s *DesktopService) attach(app *application.App, home *application.WebviewWindow) {
	s.mu.Lock()
	s.app, s.home = app, home
	s.media = newMediaEngine(s.ffmpeg, s.paths.TempDir, func(progress MediaProgress) { app.Event.Emit("media:progress", progress) })
	s.mu.Unlock()
}

func (s *DesktopService) GetConfig() AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

func (s *DesktopService) SetConfig(config AppConfig) (AppConfig, error) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.mu.Lock()
	if config.CacheDir == "" {
		config.CacheDir = s.config.CacheDir
	}
	if config.CacheLimitGB <= 0 {
		config.CacheLimitGB = 20
	}
	if config.Bounds == nil {
		config.Bounds = s.config.Bounds
	}
	s.applyPublicConfig(&config)
	if err := os.MkdirAll(config.CacheDir, 0o755); err != nil {
		s.mu.Unlock()
		return s.config, err
	}
	if err := writeJSONAtomic(s.paths.ConfigFile, persistedConfig(config)); err != nil {
		s.mu.Unlock()
		return s.config, err
	}
	s.config = config
	result := s.config
	s.mu.Unlock()
	_ = s.convergeCacheLocked()
	return result, nil
}

func (s *DesktopService) applyPublicConfig(config *AppConfig) {
	config.Backend = backendBase
	config.Stream = defaultStreamBase
	config.Version = desktopVersion
}

func persistedConfig(config AppConfig) appConfigDisk {
	return appConfigDisk{
		TaskKey: config.TaskKey, CacheDir: config.CacheDir, CacheLimitGB: config.CacheLimitGB, Bounds: config.Bounds,
	}
}

func (s *DesktopService) GetLibrary() []LibraryEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]LibraryEntry, len(s.library))
	for index := range s.library {
		result[index] = s.library[index].LibraryEntry
	}
	return result
}

func (s *DesktopService) PickAndImportVideos() (ImportResult, error) {
	paths, err := s.PickVideos()
	if err != nil || len(paths) == 0 {
		return ImportResult{}, err
	}
	return s.ImportVideos(paths), nil
}

func (s *DesktopService) PickVideos() ([]string, error) {
	s.mu.RLock()
	app, home := s.app, s.home
	s.mu.RUnlock()
	if app == nil || home == nil {
		return nil, errors.New("应用尚未初始化")
	}
	paths, err := app.Dialog.OpenFile().
		SetTitle("选择视频文件").
		AttachToWindow(home).
		AddFilter("视频文件", "*.mp4;*.m4v;*.mov;*.mkv;*.webm").
		PromptForMultipleSelection()
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	return paths, nil
}

func (s *DesktopService) ImportVideos(paths []string) ImportResult {
	result := ImportResult{Added: []LibraryEntry{}, Failed: []ImportFailure{}}
	for _, path := range paths {
		entry, err := s.importVideo(path)
		if err != nil {
			result.Failed = append(result.Failed, ImportFailure{Path: path, Name: filepath.Base(path), Error: err.Error()})
			continue
		}
		result.Added = append(result.Added, entry)
	}
	if len(result.Added) > 0 {
		s.emitLibrary()
	}
	return result
}

func (s *DesktopService) importVideo(path string) (LibraryEntry, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".mp4" && ext != ".m4v" && ext != ".mov" && ext != ".mkv" && ext != ".webm" {
		return LibraryEntry{}, fmt.Errorf("不支持的格式 %s", ext)
	}
	stat, err := os.Stat(path)
	if err != nil {
		return LibraryEntry{}, err
	}
	if !stat.Mode().IsRegular() {
		return LibraryEntry{}, errors.New("不是普通文件")
	}
	metadata, err := s.media.Probe(context.Background(), path)
	if err != nil {
		return LibraryEntry{}, err
	}
	if metadata.Duration <= 0 {
		return LibraryEntry{}, errors.New("无法读取视频时长")
	}
	if !metadata.HasVideo {
		return LibraryEntry{}, errors.New("文件中没有视频轨")
	}
	if metadata.Duration > maxVideoDuration {
		return LibraryEntry{}, fmt.Errorf("视频超过 %d 分钟上限", maxVideoDuration/60)
	}
	fingerprint, err := fileFingerprint(path, stat.Size())
	if err != nil {
		return LibraryEntry{}, err
	}

	s.mu.Lock()
	for index := range s.library {
		if s.library[index].Fingerprint == fingerprint {
			entry := s.library[index].LibraryEntry
			s.mu.Unlock()
			return entry, nil
		}
	}
	id, err := randomLibraryID()
	if err != nil {
		s.mu.Unlock()
		return LibraryEntry{}, err
	}
	now := time.Now().UnixMilli()
	entry := LibraryEntry{
		ID: id, SrcPath: path, Title: stat.Name(), Size: stat.Size(), Duration: metadata.Duration,
		Width: metadata.Width, Height: metadata.Height, Fingerprint: fingerprint, AddedAt: now, LastAccess: now,
	}
	s.library = append([]libraryDiskEntry{{LibraryEntry: entry}}, s.library...)
	if err := s.saveLibraryLocked(); err != nil {
		s.library = s.library[1:]
		s.mu.Unlock()
		return LibraryEntry{}, err
	}
	s.mu.Unlock()
	s.ensureThumbnail(entry)
	return entry, nil
}

func (s *DesktopService) OpenLibraryVideo(id string) (VideoInfo, error) {
	if !validLibraryID.MatchString(id) {
		return VideoInfo{}, errors.New("无效的媒体 ID")
	}
	s.SetOpenInEditor(id)
	s.prototype.openFormalEditor(id)
	return VideoInfo{}, nil
}

func (s *DesktopService) RemoveLibraryEntry(id string, removeCache, removeThumb bool) error {
	return s.removeLibraryData(id, LibraryRemoveOptions{Cache: removeCache, Thumb: removeThumb, Entry: true})
}

func (s *DesktopService) FFmpegStatus() FFmpegStatus {
	return s.ffmpeg.Status()
}

func (s *DesktopService) RetryFFmpeg() FFmpegStatus {
	return s.ffmpeg.Retry()
}

func (s *DesktopService) ProbeVideo(path string) (MediaMetadata, error) {
	return s.media.Probe(context.Background(), path)
}

func (s *DesktopService) ExtractAudio(id string) (AudioResult, error) {
	if !validLibraryID.MatchString(id) {
		return AudioResult{}, errors.New("无效的媒体 ID")
	}
	entry, ok := s.findEntry(id)
	if !ok {
		return AudioResult{}, errors.New("媒体库中没有这个视频")
	}
	path := entry.SrcPath
	if cached := s.cachePath(id); fileExists(cached) {
		path = cached
	}
	if !fileExists(path) {
		return AudioResult{}, errors.New("找不到本地视频文件")
	}
	metadata, err := s.media.Probe(context.Background(), path)
	if err != nil {
		return AudioResult{}, err
	}
	if !metadata.HasAudio {
		return AudioResult{}, errors.New("视频没有音轨")
	}
	return s.media.ExtractAudio(context.Background(), id, path, metadata.Duration)
}

// ComputePeaks 本地算一份波形 peaks，算完由前端传回服务端存档
func (s *DesktopService) ComputePeaks(id string) (PeaksResult, error) {
	if !validLibraryID.MatchString(id) {
		return PeaksResult{}, errors.New("无效的媒体 ID")
	}
	entry, ok := s.findEntry(id)
	if !ok {
		return PeaksResult{}, errors.New("媒体库中没有这个视频")
	}
	path := entry.SrcPath
	if cached := s.cachePath(id); fileExists(cached) {
		path = cached
	}
	if !fileExists(path) {
		return PeaksResult{}, errors.New("找不到本地视频文件")
	}
	metadata, err := s.media.Probe(context.Background(), path)
	if err != nil {
		return PeaksResult{}, err
	}
	if !metadata.HasAudio {
		return PeaksResult{}, errors.New("视频没有音轨")
	}
	return s.media.Peaks(context.Background(), path, metadata.Duration)
}

func (s *DesktopService) CancelMediaJob(id string) bool {
	cancelled := false
	s.mu.RLock()
	media := s.media
	s.mu.RUnlock()
	if media != nil {
		cancelled = media.Cancel(id)
	}
	s.videoMu.Lock()
	cancel := s.videoJobs[id]
	s.videoMu.Unlock()
	if cancel != nil {
		cancel()
		cancelled = true
	}
	return cancelled
}

func (s *DesktopService) GetCacheStats() CacheStats {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.mu.RLock()
	dir, limit := s.config.CacheDir, s.config.CacheLimitGB
	s.mu.RUnlock()
	stats := CacheStats{Dir: dir, LimitBytes: int64(limit * 1024 * 1024 * 1024)}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return stats
	}
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".mp4") && !strings.HasSuffix(entry.Name(), ".part")) {
			continue
		}
		if info, err := entry.Info(); err == nil {
			stats.Bytes += info.Size()
			if strings.HasSuffix(entry.Name(), ".mp4") {
				stats.Files++
			}
		}
	}
	return stats
}

func (s *DesktopService) HasCache(id string) bool {
	if !validLibraryID.MatchString(id) {
		return false
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	return fileExists(s.cachePath(id))
}

func (s *DesktopService) ThumbnailURL(id string) (string, error) {
	if !validLibraryID.MatchString(id) {
		return "", errors.New("无效的媒体 ID")
	}
	return s.prototype.localFileURL(s.thumbPath(id))
}

func (s *DesktopService) CacheThumbFromCloud(id, sourceURL string) bool {
	if !validLibraryID.MatchString(id) {
		return false
	}
	s.mu.RLock()
	base, client := s.streamBase, s.httpClient
	s.mu.RUnlock()
	expectedURL := strings.TrimRight(base, "/") + "/thumbs/" + url.PathEscape(id) + ".jpg"
	if sourceURL != expectedURL {
		return false
	}

	destination := s.thumbPath(id)
	if fileExists(destination) {
		return false
	}
	s.mu.Lock()
	if s.thumbJobs[id] != nil {
		s.mu.Unlock()
		return false
	}
	done := make(chan struct{})
	s.thumbJobs[id] = done
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.thumbJobs[id] == done {
			delete(s.thumbJobs, id)
			close(done)
		}
		s.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return false
	}
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 || response.ContentLength > 10<<20 {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (10<<20)+1))
	if err != nil || len(body) > 10<<20 || len(body) < 3 || body[0] != 0xff || body[1] != 0xd8 || body[2] != 0xff {
		return false
	}
	part := destination + ".part"
	_ = os.Remove(part)
	if err := os.WriteFile(part, body, 0o644); err != nil {
		return false
	}
	if fileExists(destination) {
		_ = os.Remove(part)
		return false
	}
	if err := os.Rename(part, destination); err != nil {
		_ = os.Remove(part)
		return false
	}
	s.emitThumbReady(id, destination)
	return true
}

func (s *DesktopService) CopyIntoCache(id, source string) (string, error) {
	return s.copyIntoCache(context.Background(), id, source)
}

func (s *DesktopService) copyIntoCache(ctx context.Context, id, source string) (string, error) {
	if !validLibraryID.MatchString(id) {
		return "", errors.New("无效的媒体 ID")
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	stat, err := os.Stat(source)
	if err != nil {
		return "", err
	}
	destination := s.cachePath(id)
	if sameFilePath(source, destination) {
		s.TouchCache(id)
		return destination, nil
	}
	part := destination + ".part"
	_ = os.Remove(part)
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	output, err := os.Create(part)
	if err != nil {
		return "", err
	}
	reader := &copyProgressReader{reader: &contextReader{ctx: ctx, reader: input}, total: stat.Size(), report: func(done, total int64) {
		s.reportProgress(MediaProgress{ID: id, Stage: "copy", Done: float64(done), Total: float64(total)})
	}}
	_, copyErr := io.Copy(output, reader)
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(part)
		if copyErr != nil {
			return "", copyErr
		}
		return "", closeErr
	}
	_ = os.Remove(destination)
	if err := os.Rename(part, destination); err != nil {
		_ = os.Remove(part)
		return "", err
	}
	s.TouchCache(id)
	_ = s.convergeCacheLocked()
	return destination, nil
}

func (s *DesktopService) reportProgress(progress MediaProgress) {
	s.mu.RLock()
	media, app := s.media, s.app
	s.mu.RUnlock()
	if media != nil {
		media.report(progress)
		return
	}
	if app != nil {
		app.Event.Emit("media:progress", progress)
	}
}

func (s *DesktopService) TouchCache(id string) bool {
	if !validLibraryID.MatchString(id) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.library {
		if s.library[index].ID == id {
			s.library[index].LastAccess = time.Now().UnixMilli()
			s.library[index].CloudOnly = false
			return s.saveLibraryLocked() == nil
		}
	}
	return false
}

func (s *DesktopService) ClearCache(id string) error {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if id != "" {
		if !validLibraryID.MatchString(id) {
			return errors.New("无效的媒体 ID")
		}
		if err := os.Remove(s.cachePath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	s.mu.RLock()
	dir := s.config.CacheDir
	s.mu.RUnlock()
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".mp4") || strings.HasSuffix(entry.Name(), ".part")) {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
	return nil
}

func (s *DesktopService) ensureThumbnail(entry LibraryEntry) {
	go func() {
		_ = s.ensureThumbnailReady(context.Background(), entry)
	}()
}

func (s *DesktopService) ensureThumbnailReady(ctx context.Context, entry LibraryEntry) error {
	output := s.thumbPath(entry.ID)
	if fileExists(output) {
		return nil
	}
	s.mu.Lock()
	if pending := s.thumbJobs[entry.ID]; pending != nil {
		s.mu.Unlock()
		select {
		case <-pending:
			if fileExists(output) {
				return nil
			}
			return errors.New("缩略图生成失败")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	media := s.media
	if media == nil {
		s.mu.Unlock()
		return errors.New("媒体引擎尚未初始化")
	}
	done := make(chan struct{})
	s.thumbJobs[entry.ID] = done
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.thumbJobs[entry.ID] == done {
			delete(s.thumbJobs, entry.ID)
			close(done)
		}
		s.mu.Unlock()
	}()

	path := entry.SrcPath
	if cached := s.cachePath(entry.ID); fileExists(cached) {
		path = cached
	}
	if !fileExists(path) {
		return errors.New("找不到本地视频文件")
	}
	if err := media.Thumbnail(ctx, path, output, entry.Duration); err != nil {
		return err
	}
	s.emitThumbReady(entry.ID, output)
	return nil
}

func (s *DesktopService) emitThumbReady(id, path string) {
	s.mu.RLock()
	app := s.app
	s.mu.RUnlock()
	if app != nil {
		app.Event.Emit("thumb:ready", ThumbReady{ID: id, Path: path})
	}
}

func (s *DesktopService) findEntry(id string) (LibraryEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, entry := range s.library {
		if entry.ID == id {
			return entry.LibraryEntry, true
		}
	}
	return LibraryEntry{}, false
}

func (s *DesktopService) saveLibraryLocked() error {
	return writeJSONAtomic(s.paths.LibraryFile, s.library)
}

func (s *DesktopService) emitLibrary() {
	s.mu.RLock()
	app := s.app
	s.mu.RUnlock()
	if app != nil {
		app.Event.Emit("library:changed", s.GetLibrary())
	}
}

func (s *DesktopService) cachePath(id string) string {
	s.mu.RLock()
	dir := s.config.CacheDir
	s.mu.RUnlock()
	return filepath.Join(dir, id+".mp4")
}

func (s *DesktopService) thumbPath(id string) string {
	return filepath.Join(s.paths.ThumbDir, id+".jpg")
}

func randomLibraryID() (string, error) {
	buffer := make([]byte, 6)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return "loc_" + hex.EncodeToString(buffer), nil
}

func fileFingerprint(path string, size int64) (string, error) {
	const chunkSize int64 = 2 * 1024 * 1024
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	_, _ = hash.Write([]byte(fmt.Sprintf("%d", size)))
	length := chunkSize
	if size < length {
		length = size
	}
	buffer := make([]byte, length)
	if _, err := io.ReadFull(file, buffer); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", err
	}
	_, _ = hash.Write(buffer)
	if _, err := file.Seek(maxInt64(0, size-chunkSize), io.SeekStart); err != nil {
		return "", err
	}
	buffer = make([]byte, length)
	if _, err := io.ReadFull(file, buffer); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", err
	}
	_, _ = hash.Write(buffer)
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func fileExists(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && stat.Mode().IsRegular()
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

type copyProgressReader struct {
	reader io.Reader
	done   int64
	total  int64
	last   time.Time
	report func(int64, int64)
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(buffer)
	}
}

func (r *copyProgressReader) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	r.done += int64(n)
	if time.Since(r.last) >= 200*time.Millisecond || err == io.EOF {
		r.last = time.Now()
		r.report(r.done, r.total)
	}
	return n, err
}
