package app

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	ffmpegVersion     = "6.1.1"
	ffmpegRelease     = "b6.1.1"
	ffmpegSHA256      = "04e1307997530f9cf2fe35cba2ca7e8875ca91da02f89d6c7243df819c94ad00"
	ffmpegURL         = "https://livestream.nonoka.online/desktop-updates/ffmpeg-static/b6.1.1/ffmpeg-win32-x64.gz"
	ffmpegFallbackURL = "https://github.com/eugeneware/ffmpeg-static/releases/download/b6.1.1/ffmpeg-win32-x64.gz"
	ffmpegLicenseURL  = "https://livestream.nonoka.online/desktop-updates/ffmpeg-static/b6.1.1/win32-x64.LICENSE"
)

type FFmpegStatus struct {
	State   string `json:"state"`
	Version string `json:"version"`
	Path    string `json:"path"`
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Error   string `json:"error"`
}

type ffmpegOptions struct {
	URL         string
	FallbackURL string
	LicenseURL  string
	SHA256      string
	Path        string
	Client      *http.Client
	Emit        func(FFmpegStatus)
}

type FFmpegManager struct {
	mu      sync.RWMutex
	options ffmpegOptions
	status  FFmpegStatus
	running bool
	done    chan struct{}
}

func newFFmpegManager(paths AppPaths, emit func(FFmpegStatus)) *FFmpegManager {
	name := "ffmpeg"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return newFFmpegManagerWithOptions(ffmpegOptions{
		URL:         ffmpegURL,
		FallbackURL: ffmpegFallbackURL,
		LicenseURL:  ffmpegLicenseURL,
		SHA256:      ffmpegSHA256,
		Path:        filepath.Join(paths.FFmpegDir, ffmpegRelease, name),
		Client:      &http.Client{Timeout: 20 * time.Minute},
		Emit:        emit,
	})
}

func newFFmpegManagerWithOptions(options ffmpegOptions) *FFmpegManager {
	return &FFmpegManager{
		options: options,
		status:  FFmpegStatus{State: "missing", Version: ffmpegVersion, Path: options.Path},
	}
}

func (m *FFmpegManager) Status() FFmpegStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

func (m *FFmpegManager) Start() {
	go func() { _, _ = m.Ensure(context.Background()) }()
}

func (m *FFmpegManager) Retry() FFmpegStatus {
	m.Start()
	return m.Status()
}

func (m *FFmpegManager) Ensure(ctx context.Context) (string, error) {
	m.mu.Lock()
	if m.status.State == "ready" {
		path := m.status.Path
		m.mu.Unlock()
		return path, nil
	}
	if m.running {
		done := m.done
		m.mu.Unlock()
		select {
		case <-done:
			status := m.Status()
			if status.State != "ready" {
				return "", errors.New(status.Error)
			}
			return status.Path, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	m.running = true
	m.done = make(chan struct{})
	done := m.done
	m.mu.Unlock()

	path, err := m.ensure(ctx)
	m.mu.Lock()
	m.running = false
	if err != nil {
		m.status.State = "error"
		m.status.Error = err.Error()
	} else {
		m.status.State = "ready"
		m.status.Path = path
		m.status.Error = ""
	}
	status := m.status
	close(done)
	m.mu.Unlock()
	m.emit(status)
	return path, err
}

func (m *FFmpegManager) ensure(ctx context.Context) (string, error) {
	m.setStatus("checking", 0, 0, "")
	if ok, err := verifyFileSHA256(m.options.Path, m.options.SHA256); err == nil && ok {
		go m.downloadLicense()
		return m.options.Path, nil
	}
	_ = os.Remove(m.options.Path)
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return "", fmt.Errorf("原型暂不支持自动安装 %s/%s 的 FFmpeg", runtime.GOOS, runtime.GOARCH)
	}
	if err := os.MkdirAll(filepath.Dir(m.options.Path), 0o755); err != nil {
		return "", err
	}
	part := m.options.Path + ".part"
	_ = os.Remove(part)

	resp, err := m.openDownload(ctx)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	m.setStatus("downloading", 0, resp.ContentLength, "")
	reader := &downloadProgressReader{
		reader: resp.Body,
		total:  resp.ContentLength,
		report: func(done, total int64) { m.setStatus("downloading", done, total, "") },
	}
	gz, err := gzip.NewReader(reader)
	if err != nil {
		return "", fmt.Errorf("FFmpeg 下载包无效：%w", err)
	}
	defer gz.Close()
	file, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	const maxExpandedSize = 160 << 20
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(gz, maxExpandedSize+1))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(part)
		return "", fmt.Errorf("写入 FFmpeg 失败：%w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(part)
		return "", closeErr
	}
	if written > maxExpandedSize {
		_ = os.Remove(part)
		return "", errors.New("FFmpeg 下载包解压后体积异常")
	}

	m.setStatus("verifying", resp.ContentLength, resp.ContentLength, "")
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, m.options.SHA256) {
		_ = os.Remove(part)
		return "", fmt.Errorf("FFmpeg 校验失败：SHA-256 %s", actual)
	}
	if err := os.Rename(part, m.options.Path); err != nil {
		_ = os.Remove(part)
		return "", err
	}
	_ = os.Chmod(m.options.Path, 0o755)
	go m.downloadLicense()
	return m.options.Path, nil
}

func (m *FFmpegManager) openDownload(ctx context.Context) (*http.Response, error) {
	urls := []string{m.options.URL}
	if m.options.FallbackURL != "" && m.options.FallbackURL != m.options.URL {
		urls = append(urls, m.options.FallbackURL)
	}
	var lastErr error
	for _, downloadURL := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err := m.options.Client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == http.StatusOK {
			return resp, nil
		}
		lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		resp.Body.Close()
	}
	return nil, fmt.Errorf("下载 FFmpeg 失败：%w", lastErr)
}

func (m *FFmpegManager) setStatus(state string, done, total int64, message string) {
	m.mu.Lock()
	m.status.State = state
	m.status.Done = done
	m.status.Total = total
	m.status.Error = message
	status := m.status
	m.mu.Unlock()
	m.emit(status)
}

func (m *FFmpegManager) emit(status FFmpegStatus) {
	if m.options.Emit != nil {
		m.options.Emit(status)
	}
}

func (m *FFmpegManager) downloadLicense() {
	if m.options.LicenseURL == "" {
		return
	}
	resp, err := m.options.Client.Get(m.options.LicenseURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err == nil {
		_ = writeFileAtomic(filepath.Join(filepath.Dir(m.options.Path), "LICENSE.txt"), data, 0o644)
	}
}

func verifyFileSHA256(path, expected string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, err
	}
	return strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expected), nil
}

type downloadProgressReader struct {
	reader io.Reader
	done   int64
	total  int64
	last   time.Time
	report func(int64, int64)
}

func (r *downloadProgressReader) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	r.done += int64(n)
	if time.Since(r.last) >= 200*time.Millisecond || err == io.EOF {
		r.last = time.Now()
		r.report(r.done, r.total)
	}
	return n, err
}
