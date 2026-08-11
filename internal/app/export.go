package app

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"online.nonoka.subtitle/desktop-wails/internal/platformprocess"
)

var bundledAssets fs.FS

type ExportOptions struct {
	ID      string  `json:"id"`
	T0      float64 `json:"t0"`
	T1      float64 `json:"t1"`
	ASS     string  `json:"ass"`
	CRF     int     `json:"crf"`
	Preset  string  `json:"preset"`
	ScaleH  int     `json:"scaleH"`
	ABR     string  `json:"abr"`
	OutPath string  `json:"outPath"`
}

type ExportResult struct {
	OK   bool   `json:"ok"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type exportSpec struct {
	ExportOptions
	Duration float64
}

var (
	exportPresets = map[string]bool{
		"ultrafast": true, "superfast": true, "veryfast": true, "faster": true, "fast": true,
		"medium": true, "slow": true, "slower": true, "veryslow": true,
	}
	exportBitratePattern = regexp.MustCompile(`^\d{2,4}k$`)
	invalidFilename      = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
)

func (s *DesktopService) PickExportOutput(defaultName string) (string, error) {
	s.mu.RLock()
	app := s.app
	s.mu.RUnlock()
	if app == nil {
		return "", errors.New("应用尚未初始化")
	}
	return app.Dialog.SaveFile().
		SetMessage("导出 MP4 视频").
		SetFilename(normalizeExportFilename(defaultName)).
		AddFilter("MP4 视频", "*.mp4").
		AttachToWindow(s.prototype.dialogWindow()).
		PromptForSingleSelection()
}

func (s *DesktopService) SaveSubtitle(defaultName string, content string) (string, error) {
	s.mu.RLock()
	app := s.app
	s.mu.RUnlock()
	if app == nil {
		return "", errors.New("应用尚未初始化")
	}
	path, err := app.Dialog.SaveFile().
		SetMessage("导出 ASS 字幕").
		SetFilename(normalizeSubtitleFilename(defaultName)).
		AddFilter("ASS 字幕", "*.ass").
		AttachToWindow(s.prototype.dialogWindow()).
		PromptForSingleSelection()
	if err != nil || path == "" {
		return "", err
	}
	if !strings.EqualFold(filepath.Ext(path), ".ass") {
		path += ".ass"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func normalizeSubtitleFilename(name string) string {
	name = strings.TrimSpace(filepath.Base(name))
	name = invalidFilename.ReplaceAllString(name, "_")
	if name == "" || name == "." {
		name = "subtitle.ass"
	}
	if !strings.EqualFold(filepath.Ext(name), ".ass") {
		name += ".ass"
	}
	return name
}

func normalizeExportFilename(name string) string {
	name = strings.TrimSpace(filepath.Base(name))
	name = invalidFilename.ReplaceAllString(name, "_")
	if name == "" || name == "." {
		name = "export.mp4"
	}
	if !strings.EqualFold(filepath.Ext(name), ".mp4") {
		name += ".mp4"
	}
	return name
}

func (s *DesktopService) RenderExport(options ExportOptions) (ExportResult, error) {
	spec, err := validateExportOptions(options)
	if err != nil {
		return ExportResult{}, err
	}

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	source, err := s.exportSource(spec.ID)
	if err != nil {
		return ExportResult{}, err
	}
	if sameFilePath(source, spec.OutPath) {
		return ExportResult{}, errors.New("导出位置不能覆盖原视频")
	}

	workDir, err := prepareExportWorkDir(s.paths.TempDir, spec.ID, spec.ASS)
	if err != nil {
		return ExportResult{}, err
	}
	defer os.RemoveAll(workDir)

	part := spec.OutPath + ".part"
	_ = os.Remove(part)
	jobID := "exp_" + spec.ID
	if err := s.media.renderExport(context.Background(), jobID, source, part, workDir, spec); err != nil {
		_ = os.Remove(part)
		return ExportResult{}, err
	}
	if err := replaceExportOutput(part, spec.OutPath); err != nil {
		_ = os.Remove(part)
		return ExportResult{}, err
	}
	stat, err := os.Stat(spec.OutPath)
	if err != nil {
		return ExportResult{}, err
	}
	s.reportProgress(MediaProgress{ID: jobID, Stage: "export", Done: spec.Duration, Total: spec.Duration})
	return ExportResult{OK: true, Path: spec.OutPath, Size: stat.Size()}, nil
}

func validateExportOptions(options ExportOptions) (exportSpec, error) {
	options.ID = strings.TrimSpace(options.ID)
	if !validLibraryID.MatchString(options.ID) {
		return exportSpec{}, errors.New("无效的媒体 ID")
	}
	if !isFinite(options.T0) || !isFinite(options.T1) {
		return exportSpec{}, errors.New("导出区间无效")
	}
	if options.T0 < 0 {
		options.T0 = 0
	}
	duration := options.T1 - options.T0
	if duration <= 0 {
		return exportSpec{}, errors.New("导出区间无效")
	}
	if len(options.ASS) > 64<<20 {
		return exportSpec{}, errors.New("字幕数据过大")
	}
	if options.OutPath == "" || !filepath.IsAbs(options.OutPath) {
		return exportSpec{}, errors.New("没有选择有效的保存位置")
	}
	if options.CRF <= 0 || options.CRF > 51 {
		options.CRF = 21
	}
	if !exportPresets[options.Preset] {
		options.Preset = "medium"
	}
	if options.ScaleH < 0 || options.ScaleH > 4320 {
		options.ScaleH = 0
	}
	if !exportBitratePattern.MatchString(options.ABR) {
		options.ABR = "192k"
	}
	return exportSpec{ExportOptions: options, Duration: duration}, nil
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (s *DesktopService) exportSource(id string) (string, error) {
	if cached := s.cachePath(id); fileExists(cached) {
		return cached, nil
	}
	entry, ok := s.findEntry(id)
	if ok && fileExists(entry.SrcPath) {
		return entry.SrcPath, nil
	}
	return "", errors.New("找不到本地视频文件，请重新定位")
}

func prepareExportWorkDir(tempDir, id, ass string) (string, error) {
	dir, err := os.MkdirTemp(tempDir, "nonoka_export_"+id+"_")
	if err != nil {
		return "", err
	}
	cleanup := func(err error) (string, error) {
		_ = os.RemoveAll(dir)
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "sub.ass"), []byte(ass), 0o600); err != nil {
		return cleanup(err)
	}
	fontDir := filepath.Join(dir, "fonts")
	if err := os.MkdirAll(fontDir, 0o700); err != nil {
		return cleanup(err)
	}
	if bundledAssets == nil {
		return dir, nil
	}
	entries, err := fs.ReadDir(bundledAssets, "frontend/dist/fonts")
	if err != nil {
		return dir, nil
	}
	for _, entry := range entries {
		if entry.IsDir() || !isExportFont(entry.Name()) {
			continue
		}
		data, readErr := fs.ReadFile(bundledAssets, "frontend/dist/fonts/"+entry.Name())
		if readErr != nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(fontDir, entry.Name()), data, 0o600); err != nil {
			return cleanup(err)
		}
	}
	return dir, nil
}

func isExportFont(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".ttf", ".otf", ".ttc", ".woff", ".woff2":
		return true
	default:
		return false
	}
}

func (m *MediaEngine) renderExport(ctx context.Context, jobID, input, output, workDir string, spec exportSpec) error {
	ffmpeg, err := m.ffmpeg.Ensure(ctx)
	if err != nil {
		return err
	}
	jobContext, finish := m.beginJob(ctx, jobID)
	defer finish()

	command := exec.CommandContext(jobContext, ffmpeg, buildExportArgs(input, output, spec)...)
	platformprocess.SuppressConsoleWindow(command)
	command.Dir = workDir
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
		if match == nil || time.Since(last) < 200*time.Millisecond {
			continue
		}
		microseconds, _ := strconv.ParseFloat(match[1], 64)
		done := math.Min(microseconds/1e6, spec.Duration)
		last = time.Now()
		m.report(MediaProgress{ID: jobID, Stage: "export", Done: done, Total: spec.Duration})
	}
	waitErr := command.Wait()
	if jobContext.Err() != nil {
		return errors.New("已取消")
	}
	if waitErr != nil {
		return fmt.Errorf("导出失败：%s", fallbackLine(stderr.String(), waitErr.Error()))
	}
	return scanner.Err()
}

func buildExportArgs(input, output string, spec exportSpec) []string {
	filters := []string{}
	if spec.ScaleH > 0 {
		filters = append(filters, "scale=-2:"+strconv.Itoa(spec.ScaleH))
	}
	filters = append(filters, "subtitles=sub.ass:fontsdir=fonts")
	return []string{
		"-v", "error", "-y", "-ss", strconv.FormatFloat(spec.T0, 'f', -1, 64),
		"-i", input, "-t", strconv.FormatFloat(spec.Duration, 'f', -1, 64),
		"-map", "0:v:0", "-map", "0:a:0?", "-vf", strings.Join(filters, ","),
		"-c:v", "libx264", "-preset", spec.Preset, "-crf", strconv.Itoa(spec.CRF), "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", spec.ABR, "-movflags", "+faststart",
		"-f", "mp4", "-progress", "pipe:1", "-nostats", output,
	}
}

func replaceExportOutput(source, destination string) error {
	if !fileExists(destination) {
		return os.Rename(source, destination)
	}
	backup := fmt.Sprintf("%s.nonoka-backup-%d", destination, time.Now().UnixNano())
	if err := os.Rename(destination, backup); err != nil {
		return err
	}
	if err := os.Rename(source, destination); err != nil {
		_ = os.Rename(backup, destination)
		return err
	}
	_ = os.Remove(backup)
	return nil
}
