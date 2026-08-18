package app

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	ffmpegVersion  = "6.1.1"
	ffmpegRelease  = "b6.1.1"
	ffmpegMirror   = "https://livestream.nonoka.online/desktop-updates/ffmpeg-static/" + ffmpegRelease + "/"
	ffmpegUpstream = "https://github.com/eugeneware/ffmpeg-static/releases/download/" + ffmpegRelease + "/"
)

// ffmpeg-static 各平台的包名，以及解压后二进制的 SHA-256
var ffmpegBuilds = map[string]struct{ Asset, SHA256 string }{
	"windows/amd64": {"win32-x64", "04e1307997530f9cf2fe35cba2ca7e8875ca91da02f89d6c7243df819c94ad00"},
	"darwin/arm64":  {"darwin-arm64", "a90e3db6a3fd35f6074b013f948b1aa45b31c6375489d39e572bea3f18336584"},
	"darwin/amd64":  {"darwin-x64", "ebdddc936f61e14049a2d4b549a412b8a40deeff6540e58a9f2a2da9e6b18894"},
}

type FFmpegStatus struct {
	State   string `json:"state"`
	Version string `json:"version"`
	Path    string `json:"path"`
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Error   string `json:"error"`
}

type ffmpegOptions struct {
	URL                string
	FallbackURL        string
	LicenseURL         string
	LicenseFallbackURL string
	SHA256             string
	Path               string
	Client             *http.Client
	Emit               func(FFmpegStatus)
	// 补 ad-hoc 签名，仅 darwin 需要；nil 表示不签
	Codesign func(path string) error
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
	options := ffmpegOptions{
		Path:   filepath.Join(paths.FFmpegDir, ffmpegRelease, name),
		Client: &http.Client{Timeout: 20 * time.Minute},
		Emit:   emit,
	}
	if runtime.GOOS == "darwin" {
		options.Codesign = adhocCodesign
	}
	// 镜像优先、上游兜底：镜像没同步的平台会自动落到 GitHub
	if build, ok := ffmpegBuilds[runtime.GOOS+"/"+runtime.GOARCH]; ok {
		options.URL = ffmpegMirror + "ffmpeg-" + build.Asset + ".gz"
		options.FallbackURL = ffmpegUpstream + "ffmpeg-" + build.Asset + ".gz"
		options.LicenseURL = ffmpegMirror + build.Asset + ".LICENSE"
		options.LicenseFallbackURL = ffmpegUpstream + build.Asset + ".LICENSE"
		options.SHA256 = build.SHA256
	}
	return newFFmpegManagerWithOptions(options)
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
	if m.installed() {
		go m.downloadLicense()
		return m.options.Path, nil
	}
	_ = os.Remove(m.options.Path)
	if m.options.URL == "" || m.options.SHA256 == "" {
		return "", fmt.Errorf("暂不支持自动安装 %s/%s 的 FFmpeg", runtime.GOOS, runtime.GOARCH)
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
	if err := m.sign(); err != nil {
		_ = os.Remove(m.options.Path)
		return "", err
	}
	go m.downloadLicense()
	return m.options.Path, nil
}

// 已装好的判定。macOS 上补签名会改动文件内容，官方包哈希就对不上了，
// 改用装好时记下的旁车哈希，否则每次启动都会误判损坏、重新下载。
func (m *FFmpegManager) installed() bool {
	if ok, err := verifyFileSHA256(m.options.Path, m.options.SHA256); err == nil && ok {
		return true
	}
	stamp, err := os.ReadFile(m.stampPath())
	if err != nil {
		return false
	}
	ok, err := verifyFileSHA256(m.options.Path, strings.TrimSpace(string(stamp)))
	return err == nil && ok
}

func (m *FFmpegManager) stampPath() string { return m.options.Path + ".sha256" }

// Apple Silicon 上没有签名的 arm64 可执行文件会被内核直接 SIGKILL，所以下载完补一次 ad-hoc 签名
func (m *FFmpegManager) sign() error {
	_ = os.Remove(m.stampPath())
	if m.options.Codesign == nil {
		return nil
	}
	if err := m.options.Codesign(m.options.Path); err != nil {
		// Intel 不强制签名，签不上也能跑；arm64 签不上必崩，直接报错
		if runtime.GOARCH == "arm64" {
			return err
		}
		log.Printf("ffmpeg codesign: %v", err)
		return nil
	}
	sum, err := fileSHA256(m.options.Path)
	if err != nil {
		return err
	}
	return os.WriteFile(m.stampPath(), []byte(sum), 0o644)
}

func adhocCodesign(path string) error {
	out, err := exec.Command("codesign", "--force", "--sign", "-", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("FFmpeg 签名失败：%w（%s）", err, strings.TrimSpace(string(out)))
	}
	return nil
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
	var resp *http.Response
	for _, licenseURL := range []string{m.options.LicenseURL, m.options.LicenseFallbackURL} {
		if licenseURL == "" {
			continue
		}
		got, err := m.options.Client.Get(licenseURL)
		if err == nil && got.StatusCode == http.StatusOK {
			resp = got
			break
		}
		if got != nil {
			got.Body.Close()
		}
	}
	if resp == nil {
		return
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err == nil {
		_ = writeFileAtomic(filepath.Join(filepath.Dir(m.options.Path), "LICENSE.txt"), data, 0o644)
	}
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func verifyFileSHA256(path, expected string) (bool, error) {
	actual, err := fileSHA256(path)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(actual, expected), nil
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
