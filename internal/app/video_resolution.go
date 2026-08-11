package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const defaultStreamBase = "https://livestream.nonoka.online"

type VideoResolveOptions struct {
	CanR2 bool `json:"canR2"`
}

type VideoResolution struct {
	State  string `json:"state"`
	URL    string `json:"url,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type VideoExpectation struct {
	Fingerprint string  `json:"fp"`
	Duration    float64 `json:"duration"`
}

type VideoSelection struct {
	Canceled bool   `json:"canceled,omitempty"`
	OK       bool   `json:"ok,omitempty"`
	Path     string `json:"path,omitempty"`
	Warn     string `json:"warn,omitempty"`
}

type VideoAttachResult struct {
	OK  bool   `json:"ok"`
	URL string `json:"url"`
}

type DownloadResult struct {
	OK bool `json:"ok"`
}

type VideoReady struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

type VideoFailed struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

func (s *DesktopService) ResolveVideo(id string, options VideoResolveOptions) (VideoResolution, error) {
	if !validLibraryID.MatchString(id) {
		return VideoResolution{}, errors.New("无效的媒体 ID")
	}
	if cached := s.cachePath(id); fileExists(cached) {
		info, err := s.prototype.loadVideo(cached)
		if err != nil {
			return VideoResolution{}, err
		}
		s.TouchCache(id)
		return VideoResolution{State: "cached", URL: info.URL}, nil
	}

	entry, exists := s.findEntry(id)
	if exists && fileExists(entry.SrcPath) {
		info, err := s.prototype.loadVideo(entry.SrcPath)
		if err != nil {
			return VideoResolution{}, err
		}
		s.startVideoBackground(id, func(ctx context.Context) (string, error) {
			return s.copyIntoCache(ctx, id, entry.SrcPath)
		})
		return VideoResolution{State: "source", URL: info.URL}, nil
	}
	if options.CanR2 {
		s.startVideoBackground(id, func(ctx context.Context) (string, error) {
			return s.downloadFromR2(ctx, id)
		})
		return VideoResolution{State: "downloading"}, nil
	}

	reason := "本地没有缓存，云端也未保留原视频。"
	if exists && entry.SrcPath != "" {
		reason = "原文件已不在原处，云端也未保留。"
	}
	return VideoResolution{State: "missing", Reason: reason + "请选择本地原视频文件："}, nil
}

func (s *DesktopService) DownloadFromR2(id string) (DownloadResult, error) {
	if !validLibraryID.MatchString(id) {
		return DownloadResult{}, errors.New("无效的媒体 ID")
	}
	ctx, finish, ok := s.beginVideoJob(id)
	if !ok {
		return DownloadResult{}, errors.New("该视频的后台任务正在进行")
	}
	defer finish()
	if _, err := s.downloadFromR2(ctx, id); err != nil {
		return DownloadResult{}, err
	}
	return DownloadResult{OK: true}, nil
}

func (s *DesktopService) PickAndValidateVideo(id string, expect VideoExpectation) (VideoSelection, error) {
	if !validLibraryID.MatchString(id) {
		return VideoSelection{}, errors.New("无效的媒体 ID")
	}
	s.mu.RLock()
	app := s.app
	s.mu.RUnlock()
	window := s.prototype.dialogWindow()
	if app == nil || window == nil {
		return VideoSelection{}, errors.New("应用尚未初始化")
	}
	path, err := app.Dialog.OpenFile().
		SetTitle("选择该视频的本地文件").
		AttachToWindow(window).
		AddFilter("视频", "*.mp4;*.m4v;*.mov;*.mkv;*.webm").
		PromptForSingleSelection()
	if err != nil {
		return VideoSelection{}, err
	}
	if path == "" {
		return VideoSelection{Canceled: true}, nil
	}
	return s.validateVideoSelection(path, expect)
}

func (s *DesktopService) validateVideoSelection(path string, expect VideoExpectation) (VideoSelection, error) {
	stat, err := validateVideoFile(path)
	if err != nil {
		return VideoSelection{}, err
	}
	if expect.Fingerprint != "" {
		fingerprint, err := fileFingerprint(path, stat.Size())
		if err != nil {
			return VideoSelection{}, err
		}
		if fingerprint != expect.Fingerprint {
			return VideoSelection{Path: path, Warn: "文件指纹与上传时不一致——可能不是同一个文件。"}, nil
		}
		return VideoSelection{OK: true, Path: path}, nil
	}
	if expect.Duration > 0 {
		s.mu.RLock()
		media := s.media
		s.mu.RUnlock()
		if media == nil {
			return VideoSelection{}, errors.New("媒体引擎尚未初始化")
		}
		metadata, err := media.Probe(context.Background(), path)
		if err != nil {
			return VideoSelection{}, err
		}
		difference := metadata.Duration - expect.Duration
		if difference < 0 {
			difference = -difference
		}
		if metadata.Duration > 0 && difference > 2 {
			return VideoSelection{Path: path, Warn: fmt.Sprintf("视频时长与字幕时长不符（差 %.1fs）。", difference)}, nil
		}
	}
	return VideoSelection{OK: true, Path: path}, nil
}

func (s *DesktopService) AttachLocalVideo(id, source string) (VideoAttachResult, error) {
	if !validLibraryID.MatchString(id) {
		return VideoAttachResult{}, errors.New("无效的媒体 ID")
	}
	stat, err := validateVideoFile(source)
	if err != nil {
		return VideoAttachResult{}, err
	}
	info, err := s.prototype.loadVideo(source)
	if err != nil {
		return VideoAttachResult{}, err
	}

	metadata := MediaMetadata{}
	s.mu.RLock()
	media := s.media
	s.mu.RUnlock()
	if media != nil {
		if probed, probeErr := media.Probe(context.Background(), source); probeErr == nil {
			metadata = probed
		}
	}

	now := time.Now().UnixMilli()
	s.mu.Lock()
	index := -1
	for current := range s.library {
		if s.library[current].ID == id {
			index = current
			break
		}
	}
	created := index < 0
	if created {
		s.library = append([]libraryDiskEntry{{LibraryEntry: LibraryEntry{ID: id, Title: filepath.Base(source), AddedAt: now}}}, s.library...)
		index = 0
	}
	previous := s.library[index]
	entry := &s.library[index].LibraryEntry
	entry.SrcPath = source
	entry.CloudOnly = false
	entry.Size = stat.Size()
	entry.LastAccess = now
	if metadata.Duration > 0 {
		entry.Duration = metadata.Duration
	}
	if metadata.Width > 0 {
		entry.Width = metadata.Width
	}
	if metadata.Height > 0 {
		entry.Height = metadata.Height
	}
	updated := *entry
	if err := s.saveLibraryLocked(); err != nil {
		if created {
			s.library = s.library[1:]
		} else {
			s.library[index] = previous
		}
		s.mu.Unlock()
		return VideoAttachResult{}, err
	}
	s.mu.Unlock()

	s.emitLibrary()
	s.ensureThumbnail(updated)
	s.startVideoBackground(id, func(ctx context.Context) (string, error) {
		return s.copyIntoCache(ctx, id, source)
	})
	return VideoAttachResult{OK: true, URL: info.URL}, nil
}

func (s *DesktopService) startVideoBackground(id string, task func(context.Context) (string, error)) bool {
	ctx, finish, ok := s.beginVideoJob(id)
	if !ok {
		return false
	}
	go func() {
		defer finish()
		path, err := task(ctx)
		if err != nil {
			s.emitVideoFailed(id, err)
			return
		}
		info, err := s.prototype.loadVideo(path)
		if err != nil {
			s.emitVideoFailed(id, err)
			return
		}
		s.mu.RLock()
		app := s.app
		s.mu.RUnlock()
		if app != nil {
			app.Event.Emit("video:ready", VideoReady{ID: id, URL: info.URL})
		}
	}()
	return true
}

func (s *DesktopService) beginVideoJob(id string) (context.Context, func(), bool) {
	s.videoMu.Lock()
	defer s.videoMu.Unlock()
	if s.videoJobs[id] != nil {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.videoJobs[id] = cancel
	return ctx, func() {
		cancel()
		s.videoMu.Lock()
		delete(s.videoJobs, id)
		s.videoMu.Unlock()
	}, true
}

func (s *DesktopService) emitVideoFailed(id string, err error) {
	s.mu.RLock()
	app := s.app
	s.mu.RUnlock()
	if app != nil {
		app.Event.Emit("video:failed", VideoFailed{ID: id, Error: err.Error()})
	}
}

func (s *DesktopService) downloadFromR2(ctx context.Context, id string) (string, error) {
	if !validLibraryID.MatchString(id) {
		return "", errors.New("无效的媒体 ID")
	}
	s.mu.RLock()
	base, client := s.streamBase, s.httpClient
	s.mu.RUnlock()
	if client == nil {
		client = http.DefaultClient
	}
	requestURL := strings.TrimRight(base, "/") + "/uploads/" + url.PathEscape(id) + ".mp4"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("云端取回失败（HTTP %d）", response.StatusCode)
	}

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	destination := s.cachePath(id)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", err
	}
	part := destination + ".part"
	_ = os.Remove(part)
	output, err := os.Create(part)
	if err != nil {
		return "", err
	}
	keepPart := false
	defer func() {
		_ = output.Close()
		if !keepPart {
			_ = os.Remove(part)
		}
	}()

	total := response.ContentLength
	if total < 0 {
		total = 0
	}
	reader := &copyProgressReader{reader: &contextReader{ctx: ctx, reader: response.Body}, total: total, report: func(done, total int64) {
		s.reportProgress(MediaProgress{ID: id, Stage: "download", Done: float64(done), Total: float64(total)})
	}}
	written, copyErr := io.Copy(output, reader)
	closeErr := output.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if response.ContentLength > 0 && written != response.ContentLength {
		return "", fmt.Errorf("下载不完整（%d/%d）", written, response.ContentLength)
	}
	if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.Rename(part, destination); err != nil {
		return "", err
	}
	keepPart = true
	s.reportProgress(MediaProgress{ID: id, Stage: "download", Done: float64(written), Total: float64(written)})
	s.TouchCache(id)
	if entry, ok := s.findEntry(id); ok {
		s.ensureThumbnail(entry)
	}
	_ = s.convergeCacheLocked()
	return destination, nil
}

func validateVideoFile(path string) (os.FileInfo, error) {
	extension := strings.ToLower(filepath.Ext(path))
	switch extension {
	case ".mp4", ".m4v", ".mov", ".mkv", ".webm":
	default:
		return nil, errUnsupportedVideo
	}
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !stat.Mode().IsRegular() {
		return nil, errors.New("不是普通文件")
	}
	return stat, nil
}

func sameFilePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
	}
	return filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}
