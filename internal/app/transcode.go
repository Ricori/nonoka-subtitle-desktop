package app

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"online.nonoka.subtitle/desktop/internal/platformprocess"
)

type TranscodeResult struct {
	OK  bool   `json:"ok"`
	URL string `json:"url"`
}

/**
 * 转码兜底：HEVC/H.265、AV1、10bit 这些 Chromium 得靠系统解码器才放得出来，缺了就只有
 * 报错。转成 H.264/AAC 覆盖缓存副本（<id>.mp4），解析链下次照旧命中缓存直接播。
 */
func (s *DesktopService) TranscodeToH264(id string) (TranscodeResult, error) {
	if !validLibraryID.MatchString(id) {
		return TranscodeResult{}, errors.New("无效的媒体 ID")
	}
	s.mu.RLock()
	media := s.media
	s.mu.RUnlock()
	if media == nil {
		return TranscodeResult{}, errors.New("媒体引擎尚未初始化")
	}
	source, err := s.transcodeSource(id)
	if err != nil {
		return TranscodeResult{}, err
	}
	// 和后台复制/云端取回互斥——它们写的是同一个缓存文件
	ctx, finish, ok := s.beginVideoJob(id)
	if !ok {
		return TranscodeResult{}, errors.New("该视频的后台任务正在进行")
	}
	defer finish()

	metadata, err := media.Probe(ctx, source)
	if err != nil {
		return TranscodeResult{}, err
	}
	if !metadata.HasVideo {
		return TranscodeResult{}, errors.New("文件里没有视频轨")
	}

	destination := s.cachePath(id)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return TranscodeResult{}, err
	}
	// 转码要跑好几分钟，不能全程占着 cacheMu。产物先落在缓存目录里的临时名下，
	// 换名那一下才上锁（同目录 rename 才不会跨盘失败）
	part := destination + ".h264.part"
	_ = os.Remove(part)
	if err := media.transcodeH264(ctx, id, source, part, metadata.Duration); err != nil {
		_ = os.Remove(part)
		return TranscodeResult{}, err
	}

	if err := s.installTranscoded(id, part, destination); err != nil {
		_ = os.Remove(part)
		return TranscodeResult{}, err
	}
	info, err := s.prototype.loadVideo(destination)
	if err != nil {
		return TranscodeResult{}, err
	}
	return TranscodeResult{OK: true, URL: info.URL}, nil
}

// 转码源优先用原文件（画质最好），原文件不在了才退回缓存副本
func (s *DesktopService) transcodeSource(id string) (string, error) {
	entry, ok := s.findEntry(id)
	if ok && fileExists(entry.SrcPath) {
		return entry.SrcPath, nil
	}
	if cached := s.cachePath(id); fileExists(cached) {
		return cached, nil
	}
	return "", errors.New("找不到本地视频文件，请重新定位")
}

func (s *DesktopService) installTranscoded(id, part, destination string) error {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(part, destination); err != nil {
		return err
	}
	s.TouchCache(id)
	_ = s.convergeCacheLocked()
	return nil
}

func (m *MediaEngine) transcodeH264(ctx context.Context, id, input, output string, duration float64) error {
	ffmpeg, err := m.ffmpeg.Ensure(ctx)
	if err != nil {
		return err
	}
	jobContext, finish := m.beginJob(ctx, id)
	defer finish()

	command := exec.CommandContext(jobContext, ffmpeg, buildTranscodeArgs(input, output)...)
	platformprocess.SuppressConsoleWindow(command)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	last := time.Time{}
	for scanner.Scan() {
		match := progressPattern.FindStringSubmatch(scanner.Text())
		if match == nil || duration <= 0 || time.Since(last) < 200*time.Millisecond {
			continue
		}
		microseconds, _ := strconv.ParseFloat(match[1], 64)
		last = time.Now()
		m.report(MediaProgress{ID: id, Stage: "transcode", Done: math.Min(microseconds/1e6, duration), Total: duration})
	}
	waitErr := command.Wait()
	if jobContext.Err() != nil {
		return errors.New("已取消")
	}
	if waitErr != nil {
		return fmt.Errorf("转码失败：%s", fallbackLine(stderr.String(), waitErr.Error()))
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	m.report(MediaProgress{ID: id, Stage: "transcode", Done: duration, Total: duration})
	return nil
}

// 只换编码不动分辨率；yuv420p 把 10bit 压回 8bit，否则浏览器照样解不了
func buildTranscodeArgs(input, output string) []string {
	return []string{
		"-v", "error", "-y", "-i", input,
		"-map", "0:v:0", "-map", "0:a:0?", "-sn", "-dn",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "21", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "192k", "-movflags", "+faststart",
		"-f", "mp4", "-progress", "pipe:1", "-nostats", output,
	}
}
