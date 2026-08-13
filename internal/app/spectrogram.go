package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"online.nonoka.subtitle/desktop/internal/platformprocess"
)

const (
	spectrogramWidth  = 8192
	spectrogramHeight = 384
)

type SpectrogramTileResult struct {
	URL      string  `json:"url"`
	Start    float64 `json:"start"`
	Duration float64 `json:"duration"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
}

// SpectrogramTile 按需生成当前时间轴需要的频谱切片。图片只写本地临时缓存。
func (s *DesktopService) SpectrogramTile(id string, start, duration float64) (SpectrogramTileResult, error) {
	if !validLibraryID.MatchString(id) {
		return SpectrogramTileResult{}, errors.New("无效的媒体 ID")
	}
	if math.IsNaN(start) || math.IsInf(start, 0) || math.IsNaN(duration) || math.IsInf(duration, 0) ||
		start < 0 || duration < 0.25 || duration > 6000 {
		return SpectrogramTileResult{}, errors.New("无效的频谱时间范围")
	}
	entry, ok := s.findEntry(id)
	if !ok {
		return SpectrogramTileResult{}, errors.New("媒体库中没有这个视频")
	}
	path := entry.SrcPath
	if cached := s.cachePath(id); fileExists(cached) {
		path = cached
	}
	if !fileExists(path) {
		return SpectrogramTileResult{}, errors.New("频谱图需要本地视频文件")
	}

	s.mu.RLock()
	media := s.media
	s.mu.RUnlock()
	if media == nil {
		return SpectrogramTileResult{}, errors.New("媒体引擎尚未初始化")
	}
	total := entry.Duration
	if total <= 0 {
		metadata, err := media.Probe(context.Background(), path)
		if err != nil {
			return SpectrogramTileResult{}, err
		}
		if !metadata.HasAudio {
			return SpectrogramTileResult{}, errors.New("视频没有音轨")
		}
		total = metadata.Duration
	}
	start, duration, err := clampSpectrogramRange(start, duration, total)
	if err != nil {
		return SpectrogramTileResult{}, err
	}

	stat, err := os.Stat(path)
	if err != nil {
		return SpectrogramTileResult{}, err
	}
	startMS := int64(math.Round(start * 1000))
	durationMS := int64(math.Round(duration * 1000))
	dir := filepath.Join(s.paths.TempDir, "spectrogram", id)
	name := fmt.Sprintf("v8-%x-%x-%d-%d.png", stat.Size(), stat.ModTime().UnixNano(), startMS, durationMS)
	output := filepath.Join(dir, name)

	// 同一时间只跑一个频谱任务，避免快速滚动时多个 FFmpeg 抢满 CPU。
	s.spectrogramMu.Lock()
	defer s.spectrogramMu.Unlock()
	if !fileExists(output) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return SpectrogramTileResult{}, err
		}
		part := output + ".part.png"
		_ = os.Remove(part)
		if err := media.Spectrogram(context.Background(), path, part, start, duration); err != nil {
			_ = os.Remove(part)
			return SpectrogramTileResult{}, err
		}
		if err := os.Rename(part, output); err != nil {
			_ = os.Remove(part)
			return SpectrogramTileResult{}, err
		}
	}
	now := time.Now()
	_ = os.Chtimes(output, now, now)
	trimSpectrogramCache(dir, output, 48)
	url, err := s.prototype.localFileURL(output)
	if err != nil {
		return SpectrogramTileResult{}, err
	}
	return SpectrogramTileResult{
		URL: url, Start: start, Duration: duration, Width: spectrogramWidth, Height: spectrogramHeight,
	}, nil
}

type spectrogramCacheEntry struct {
	path    string
	modTime time.Time
}

func trimSpectrogramCache(dir, keep string, limit int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	files := make([]spectrogramCacheEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".png") {
			continue
		}
		if info, infoErr := entry.Info(); infoErr == nil {
			files = append(files, spectrogramCacheEntry{path: filepath.Join(dir, entry.Name()), modTime: info.ModTime()})
		}
	}
	if len(files) <= limit {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.Before(files[j].modTime) })
	remove := len(files) - limit
	for _, file := range files {
		if remove == 0 {
			break
		}
		if file.path == keep {
			continue
		}
		_ = os.Remove(file.path)
		remove--
	}
}

func clampSpectrogramRange(start, duration, total float64) (float64, float64, error) {
	start = math.Round(start*1000) / 1000
	duration = math.Round(duration*1000) / 1000
	if total <= 0 || start >= total {
		return 0, 0, errors.New("频谱时间超出视频范围")
	}
	if end := start + duration; end > total {
		duration = math.Round((total-start)*1000) / 1000
	}
	if duration < 0.01 {
		return 0, 0, errors.New("频谱时间范围过短")
	}
	return start, duration, nil
}

func (m *MediaEngine) Spectrogram(ctx context.Context, input, output string, start, duration float64) error {
	ffmpeg, err := m.ffmpeg.Ensure(ctx)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, ffmpeg, spectrogramArgs(input, output, start, duration)...)
	platformprocess.SuppressConsoleWindow(command)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("生成频谱图失败：%s", fallbackLine(stderr.String(), err.Error()))
	}
	return nil
}

func spectrogramArgs(input, output string, start, duration float64) []string {
	filter := fmt.Sprintf(
		"showspectrumpic=s=%dx%d:legend=disabled:mode=combined:color=green:scale=log:fscale=log:gain=2:drange=78,"+
			"lutrgb=r='33+0.870588*val':g='19+0.92549*val':b='63+0.752941*val'",
		spectrogramWidth, spectrogramHeight,
	)
	return []string{
		"-v", "error", "-ss", strconv.FormatFloat(start, 'f', 3, 64),
		"-t", strconv.FormatFloat(duration, 'f', 3, 64), "-i", input,
		"-lavfi", filter, "-frames:v", "1", "-y", output,
	}
}
