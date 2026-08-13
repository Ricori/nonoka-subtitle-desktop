package app

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"online.nonoka.subtitle/desktop/internal/platformprocess"
)

type MediaMetadata struct {
	Duration float64 `json:"duration"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	HasVideo bool    `json:"hasVideo"`
	HasAudio bool    `json:"hasAudio"`
}

type MediaProgress struct {
	ID    string  `json:"id"`
	Stage string  `json:"stage"`
	Done  float64 `json:"done"`
	Total float64 `json:"total"`
}

type AudioResult struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type MediaEngine struct {
	ffmpeg  *FFmpegManager
	tempDir string
	emit    func(MediaProgress)
	mu      sync.Mutex
	jobs    map[string]*mediaJob
}

type mediaJob struct {
	cancel context.CancelFunc
}

var (
	durationPattern   = regexp.MustCompile(`Duration:\s*(\d+):(\d\d):(\d\d(?:\.\d+)?)`)
	resolutionPattern = regexp.MustCompile(`,\s*(\d{2,5})x(\d{2,5})`)
	progressPattern   = regexp.MustCompile(`^out_time_us=(\d+)$`)
)

func newMediaEngine(ffmpeg *FFmpegManager, tempDir string, emit func(MediaProgress)) *MediaEngine {
	return &MediaEngine{ffmpeg: ffmpeg, tempDir: tempDir, emit: emit, jobs: make(map[string]*mediaJob)}
}

func (m *MediaEngine) Probe(ctx context.Context, input string) (MediaMetadata, error) {
	ffmpeg, err := m.ffmpeg.Ensure(ctx)
	if err != nil {
		return MediaMetadata{}, err
	}
	command := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-i", input)
	platformprocess.SuppressConsoleWindow(command)
	output, _ := command.CombinedOutput()
	return parseMediaMetadata(string(output))
}

func parseMediaMetadata(text string) (MediaMetadata, error) {
	if !strings.Contains(text, "Input #0") {
		line := lastLine(text)
		if line == "" {
			line = "无法读取文件"
		}
		return MediaMetadata{}, errors.New(line)
	}

	metadata := MediaMetadata{}
	if match := durationPattern.FindStringSubmatch(text); match != nil {
		hours, _ := strconv.ParseFloat(match[1], 64)
		minutes, _ := strconv.ParseFloat(match[2], 64)
		seconds, _ := strconv.ParseFloat(match[3], 64)
		metadata.Duration = hours*3600 + minutes*60 + seconds
	}
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, "Stream #") {
			continue
		}
		if strings.Contains(line, ": Video:") && !metadata.HasVideo {
			metadata.HasVideo = true
			if match := resolutionPattern.FindStringSubmatch(line); match != nil {
				metadata.Width, _ = strconv.Atoi(match[1])
				metadata.Height, _ = strconv.Atoi(match[2])
			}
		}
		if strings.Contains(line, ": Audio:") {
			metadata.HasAudio = true
		}
	}
	return metadata, nil
}

func (m *MediaEngine) Thumbnail(ctx context.Context, input, output string, duration float64) error {
	ffmpeg, err := m.ffmpeg.Ensure(ctx)
	if err != nil {
		return err
	}
	at := 1.0
	if duration > 10 {
		at = duration * 0.1
		if at > 60 {
			at = 60
		}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, ffmpeg,
		"-v", "error", "-ss", strconv.FormatFloat(at, 'f', -1, 64), "-i", input,
		"-frames:v", "1", "-vf", "scale=480:-2", "-q:v", "4", "-y", output,
	)
	platformprocess.SuppressConsoleWindow(command)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("生成缩略图失败：%s", fallbackLine(string(output), err.Error()))
	}
	return nil
}

func (m *MediaEngine) ExtractAudio(ctx context.Context, id, input string, duration float64) (AudioResult, error) {
	ffmpeg, err := m.ffmpeg.Ensure(ctx)
	if err != nil {
		return AudioResult{}, err
	}
	output := filepath.Join(m.tempDir, audioTempPrefix+id+".m4a")
	jobContext, finish := m.beginJob(ctx, id)
	defer finish()

	if err := m.runExtract(jobContext, ffmpeg, id, input, output, duration, true); err != nil {
		if jobContext.Err() != nil {
			_ = os.Remove(output)
			return AudioResult{}, errors.New("已取消")
		}
		if err := m.runExtract(jobContext, ffmpeg, id, input, output, duration, false); err != nil {
			_ = os.Remove(output)
			return AudioResult{}, err
		}
	}
	stat, err := os.Stat(output)
	if err != nil {
		return AudioResult{}, err
	}
	m.report(MediaProgress{ID: id, Stage: "audio", Done: duration, Total: duration})
	return AudioResult{Path: output, Size: stat.Size()}, nil
}

func (m *MediaEngine) Cancel(id string) bool {
	m.mu.Lock()
	job := m.jobs[id]
	m.mu.Unlock()
	if job == nil {
		return false
	}
	job.cancel()
	return true
}

func (m *MediaEngine) beginJob(ctx context.Context, id string) (context.Context, func()) {
	jobContext, cancel := context.WithCancel(ctx)
	job := &mediaJob{cancel: cancel}
	m.mu.Lock()
	previous := m.jobs[id]
	m.jobs[id] = job
	m.mu.Unlock()
	if previous != nil {
		previous.cancel()
	}
	return jobContext, func() {
		cancel()
		m.mu.Lock()
		if m.jobs[id] == job {
			delete(m.jobs, id)
		}
		m.mu.Unlock()
	}
}

func (m *MediaEngine) runExtract(ctx context.Context, ffmpeg, id, input, output string, duration float64, copyAudio bool) error {
	codec := []string{"-c:a", "copy"}
	if !copyAudio {
		codec = []string{"-c:a", "aac", "-b:a", "160k"}
	}
	args := []string{"-v", "error", "-y", "-i", input, "-vn", "-sn", "-dn"}
	args = append(args, codec...)
	args = append(args, "-movflags", "+faststart", "-progress", "pipe:1", "-nostats", output)
	command := exec.CommandContext(ctx, ffmpeg, args...)
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
		done := microseconds / 1e6
		if done > duration {
			done = duration
		}
		last = time.Now()
		m.report(MediaProgress{ID: id, Stage: "audio", Done: done, Total: duration})
	}
	if err := command.Wait(); err != nil {
		return fmt.Errorf("抽取音频失败：%s", fallbackLine(stderr.String(), err.Error()))
	}
	return scanner.Err()
}

func (m *MediaEngine) report(progress MediaProgress) {
	if m.emit != nil {
		m.emit(progress)
	}
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[len(lines)-1])
}

func fallbackLine(text, fallback string) string {
	if line := lastLine(text); line != "" {
		return line
	}
	return fallback
}

// ── 波形 peaks ────────────────────────────────────────────────────────────────
// 云端转写时由 worker 顺手算（见 vod/workers/video.py 的 _generate_peaks）；导入已有
// 字幕那条路根本不上传音频，云端算不出来，只能本地算一份再传上去。两边的算法必须逐字
// 一致——同一个视频换条路导入就换一副波形，用户会以为轴对不上。

const (
	peaksSampleRate = 16000 // 与云端同频：桶宽按采样率算，不一致会整体错位
	peaksPerSecond  = 20    // 每秒桶数，编辑器时间轴按这个密度画
	peaksMaxBuckets = 60000 // 上限，超长素材降密度而不是无限撑大 JSON
)

type PeaksResult struct {
	PerSec   float64   `json:"per_sec"`
	Duration float64   `json:"duration"`
	Peaks    []float64 `json:"peaks"`
}

// 一个桶的峰值：桶内样本绝对值的最大值，归一化到 0~1（保留三位小数，够画图又省一半体积）
func bucketPeak(raw []byte) float64 {
	peak := 0
	for i := 0; i+1 < len(raw); i += 2 {
		value := int(int16(uint16(raw[i]) | uint16(raw[i+1])<<8))
		if value < 0 {
			value = -value
		}
		if value > peak {
			peak = value
		}
	}
	return math.Round(float64(peak)/32768.0*1000) / 1000
}

// Peaks 解出 16k 单声道 PCM，逐桶取最大振幅。用原始混音而不是分离后的人声——云端那边
// 也是这么算的，波形要反映的是「这里有没有声音」，不是「这里有没有人声」。
func (m *MediaEngine) Peaks(ctx context.Context, input string, duration float64) (PeaksResult, error) {
	ffmpeg, err := m.ffmpeg.Ensure(ctx)
	if err != nil {
		return PeaksResult{}, err
	}
	bucket := peaksSampleRate / peaksPerSecond
	if duration > 0 {
		target := int(duration * peaksPerSecond)
		if target < 1 {
			target = 1
		} else if target > peaksMaxBuckets {
			target = peaksMaxBuckets
		}
		if bucket = int(math.Ceil(duration * peaksSampleRate / float64(target))); bucket < 1 {
			bucket = 1
		}
	}
	command := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-i", input,
		"-vn", "-ac", "1", "-ar", strconv.Itoa(peaksSampleRate), "-f", "s16le", "pipe:1")
	platformprocess.SuppressConsoleWindow(command)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return PeaksResult{}, err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return PeaksResult{}, err
	}
	reader := bufio.NewReaderSize(stdout, 1<<20)
	peaks := make([]float64, 0, peaksPerSecond*int(duration)+8)
	raw := make([]byte, bucket*2)
	for {
		read, err := io.ReadFull(reader, raw)
		if read >= 2 {
			peaks = append(peaks, bucketPeak(raw[:read/2*2]))
		}
		if err != nil { // EOF / ErrUnexpectedEOF：末尾不足一桶，上面已经收了
			break
		}
	}
	if err := command.Wait(); err != nil {
		return PeaksResult{}, fmt.Errorf("计算波形失败：%s", fallbackLine(stderr.String(), err.Error()))
	}
	if len(peaks) == 0 {
		return PeaksResult{}, errors.New("音频里没有可用的采样")
	}
	// per_sec 报**实际**密度而不是名义值：桶宽是向上取整来的，两者能差百分之几，
	// 编辑器拿名义值定位会在片尾累积出肉眼可见的偏移。
	perSecond := float64(peaksPerSecond)
	if duration > 0 {
		perSecond = math.Round(float64(len(peaks))/duration*1000) / 1000
	}
	return PeaksResult{PerSec: perSecond, Duration: math.Round(duration*1000) / 1000, Peaks: peaks}, nil
}
