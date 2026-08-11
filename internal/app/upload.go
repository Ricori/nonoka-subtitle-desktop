package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const audioTempPrefix = "nonoka_audio_"

type UploadResult struct {
	OK     bool `json:"ok"`
	Status int  `json:"status"`
}

func (s *DesktopService) UploadFile(id, filePath, putURL string, headers map[string]string) (UploadResult, error) {
	if !validLibraryID.MatchString(id) {
		return UploadResult{}, errors.New("无效的媒体 ID")
	}
	if !isOwnedAudioTemp(filePath, s.paths.TempDir) {
		return UploadResult{}, errors.New("只能上传 Nonoka 生成的临时音频")
	}
	target, err := validateUploadURL(putURL)
	if err != nil {
		return UploadResult{}, err
	}
	input, err := os.Open(filePath)
	if err != nil {
		return UploadResult{}, err
	}
	defer input.Close()
	stat, err := input.Stat()
	if err != nil {
		return UploadResult{}, err
	}

	s.mu.RLock()
	media, client := s.media, s.httpClient
	s.mu.RUnlock()
	if media == nil {
		return UploadResult{}, errors.New("媒体引擎尚未初始化")
	}
	if client == nil {
		client = http.DefaultClient
	}
	jobContext, finish := media.beginJob(context.Background(), id)
	defer finish()
	reader := &copyProgressReader{reader: &contextReader{ctx: jobContext, reader: input}, total: stat.Size(), report: func(done, total int64) {
		s.reportProgress(MediaProgress{ID: id, Stage: "upload", Done: float64(done), Total: float64(total)})
	}}
	request, err := http.NewRequestWithContext(jobContext, http.MethodPut, target.String(), reader)
	if err != nil {
		return UploadResult{}, err
	}
	request.ContentLength = stat.Size()
	for name, value := range headers {
		if !strings.EqualFold(name, "Content-Length") {
			request.Header.Set(name, value)
		}
	}
	response, err := client.Do(request)
	if err != nil {
		if jobContext.Err() != nil {
			return UploadResult{}, errors.New("已取消")
		}
		return UploadResult{}, err
	}
	defer response.Body.Close()
	body := responseSnippet(response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := fmt.Sprintf("上传失败（HTTP %d）", response.StatusCode)
		if body != "" {
			message += "：" + body
		}
		return UploadResult{}, errors.New(message)
	}
	s.reportProgress(MediaProgress{ID: id, Stage: "upload", Done: float64(stat.Size()), Total: float64(stat.Size())})
	return UploadResult{OK: true, Status: response.StatusCode}, nil
}

func (s *DesktopService) UploadThumb(id, putURL string) (UploadResult, error) {
	if !validLibraryID.MatchString(id) {
		return UploadResult{}, errors.New("无效的媒体 ID")
	}
	target, err := validateUploadURL(putURL)
	if err != nil {
		return UploadResult{}, err
	}
	entry, ok := s.findEntry(id)
	if !ok {
		return UploadResult{}, errors.New("媒体库中没有这个视频")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := s.ensureThumbnailReady(ctx, entry); err != nil {
		return UploadResult{OK: false}, err
	}
	body, err := os.ReadFile(s.thumbPath(id))
	if err != nil {
		return UploadResult{OK: false}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, target.String(), bytes.NewReader(body))
	if err != nil {
		return UploadResult{}, err
	}
	request.ContentLength = int64(len(body))
	request.Header.Set("Content-Type", "image/jpeg")
	request.Header.Set("Cache-Control", "public, max-age=31536000, immutable")
	s.mu.RLock()
	client := s.httpClient
	s.mu.RUnlock()
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return UploadResult{}, err
	}
	defer response.Body.Close()
	_ = responseSnippet(response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return UploadResult{}, fmt.Errorf("封面上传失败（HTTP %d）", response.StatusCode)
	}
	return UploadResult{OK: true, Status: response.StatusCode}, nil
}

func (s *DesktopService) DeleteTemp(path string) bool {
	if !isOwnedAudioTemp(path, s.paths.TempDir) {
		return false
	}
	return os.Remove(path) == nil
}

func validateUploadURL(raw string) (*url.URL, error) {
	target, err := url.Parse(raw)
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
		return nil, errors.New("无效的上传地址")
	}
	if target.User != nil {
		return nil, errors.New("上传地址不能包含用户信息")
	}
	return target, nil
}

func isOwnedAudioTemp(path, tempDir string) bool {
	if path == "" {
		return false
	}
	absolute, err := filepath.Abs(path)
	if err != nil || !sameFilePath(filepath.Dir(absolute), tempDir) {
		return false
	}
	name := filepath.Base(absolute)
	return strings.HasPrefix(name, audioTempPrefix) && strings.EqualFold(filepath.Ext(name), ".m4a")
}

func responseSnippet(reader io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(reader, 501))
	return strings.TrimSpace(string(data[:min(len(data), 500)]))
}
