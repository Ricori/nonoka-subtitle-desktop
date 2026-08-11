package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/endpoint"
)

type UpdateStatus struct {
	Mandatory bool   `json:"mandatory"`
	Version   string `json:"version"`
	Stage     string `json:"stage"`
	Done      int64  `json:"done"`
	Total     int64  `json:"total"`
	Ready     bool   `json:"ready"`
}

type ReleaseNotes struct {
	Version string `json:"version"`
	Notes   string `json:"notes"`
}

type pendingUpdate struct {
	Version string `json:"version"`
}

func cleanupReplacedExecutables() {
	if runtime.GOOS != "windows" {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		return
	}
	go func() {
		// 更新助手退出前旧 EXE 仍被占用；新版启动后短暂重试即可清掉。
		for attempt := 0; attempt < 60; attempt++ {
			if removeReplacedExecutables(executable) == 0 {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()
}

func removeReplacedExecutables(executable string) int {
	directory, name := filepath.Dir(executable), filepath.Base(executable)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return 0
	}
	prefix := name + ".old."
	remaining := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		if _, err := strconv.ParseInt(strings.TrimPrefix(entry.Name(), prefix), 10, 64); err != nil {
			continue
		}
		if err := os.Remove(filepath.Join(directory, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			remaining++
		}
	}
	return remaining
}

func (s *DesktopService) initAutoUpdate(app *application.App) error {
	manifestURL := os.Getenv("NONOKA_WAILS_UPDATE_MANIFEST")
	if manifestURL == "" {
		base := os.Getenv("NONOKA_UPDATE_URL")
		if base == "" {
			base = defaultStreamBase + "/desktop-updates"
		}
		manifestURL = strings.TrimRight(base, "/") + "/wails/latest.json"
	}
	provider, err := endpoint.New(endpoint.Config{URL: manifestURL, Channel: "stable"})
	if err != nil {
		return err
	}
	if err := app.Updater.Init(updater.Config{
		CurrentVersion: desktopVersion,
		Providers:      []updater.Provider{provider},
		Window:         updater.WindowNone,
	}); err != nil {
		return err
	}
	app.Event.On(updater.EventDownloadProgress, func(event *application.CustomEvent) {
		progress, ok := event.Data.(updater.Progress)
		if !ok {
			return
		}
		s.updateMu.Lock()
		status := s.updateStatus
		status.Stage = "download"
		status.Done = progress.Written
		status.Total = progress.Total
		s.updateStatus = status
		s.updateMu.Unlock()
		s.publishUpdateStatus(status)
	})
	app.Event.On(updater.EventVerifying, func(_ *application.CustomEvent) {
		s.setUpdateStage("verify", false)
	})
	app.Event.On(updater.EventInstalling, func(_ *application.CustomEvent) {
		s.setUpdateStage("unpack", false)
	})
	return nil
}

func (s *DesktopService) startAutoUpdate() {
	if os.Getenv("NONOKA_DISABLE_UPDATE") == "1" || os.Getenv("NONOKA_FORMAL_MOCK") == "1" {
		return
	}
	s.setUpdateStage("checking", false)
	// 只有上次启动已下载好更新（留下待安装标记）时，这次启动才直接安装重启；
	// 否则本次检查到的更新只下载等待，安装留到下次启动，不打断当前会话。
	installNow := s.consumePendingUpdate()
	go func() {
		s.checkForUpdate(context.Background(), installNow)
		for {
			time.Sleep(4 * time.Hour)
			s.checkForUpdate(context.Background(), false)
		}
	}()
}

func (s *DesktopService) checkForUpdate(ctx context.Context, installIfReady bool) {
	s.updateMu.Lock()
	if s.updateBusy || s.updateStatus.Ready {
		s.updateMu.Unlock()
		return
	}
	s.updateBusy = true
	s.updateMu.Unlock()
	defer func() {
		s.updateMu.Lock()
		s.updateBusy = false
		s.updateMu.Unlock()
	}()

	s.mu.RLock()
	app := s.app
	s.mu.RUnlock()
	if app == nil {
		return
	}
	release, err := app.Updater.Check(ctx)
	if err != nil {
		s.updateMu.Lock()
		mandatory := s.updateStatus.Mandatory
		s.updateMu.Unlock()
		if mandatory {
			s.setUpdateStage("retry", false)
			s.scheduleUpdateRetry(installIfReady)
		} else {
			s.setUpdateStage("idle", false)
		}
		return
	}
	if release == nil {
		s.updateMu.Lock()
		s.updateStatus = UpdateStatus{Stage: "idle"}
		status := s.updateStatus
		s.updateMu.Unlock()
		s.publishUpdateStatus(status)
		return
	}
	mandatory := releaseMandatory(release, desktopVersion)
	s.updateMu.Lock()
	s.updateStatus = UpdateStatus{
		Mandatory: mandatory, Version: release.Version, Stage: "download", Total: release.Artifact.Size,
	}
	status := s.updateStatus
	s.updateMu.Unlock()
	s.publishUpdateStatus(status)

	if err := app.Updater.DownloadAndInstall(ctx); err != nil {
		if mandatory {
			s.setUpdateStage("retry", false)
			s.scheduleUpdateRetry(installIfReady)
		} else {
			s.setUpdateStage("idle", false)
		}
		return
	}
	if strings.TrimSpace(release.Notes) != "" {
		_ = writeJSONAtomic(s.releaseNotesPath(), ReleaseNotes{Version: release.Version, Notes: release.Notes})
	}
	s.updateMu.Lock()
	s.updateStatus.Stage = "ready"
	s.updateStatus.Ready = true
	status = s.updateStatus
	s.updateMu.Unlock()
	s.publishUpdateStatus(status)
	app.Event.Emit("update:ready", map[string]string{"version": release.Version})
	if mandatory {
		go s.waitAndInstallMandatory()
	} else if installIfReady {
		// 上次启动就已下载好，这次启动直接安装并重启进入新版本。
		go s.InstallUpdate()
	} else {
		// 本次会话内下载完成：不打断当前使用，留到下次启动再安装。
		s.markPendingUpdate(release.Version)
	}
}

func (s *DesktopService) scheduleUpdateRetry(installIfReady bool) {
	go func() {
		time.Sleep(time.Minute)
		s.checkForUpdate(context.Background(), installIfReady)
	}()
}

func (s *DesktopService) GetUpdateStatus() UpdateStatus {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	status := s.updateStatus
	if status.Stage == "" {
		status.Stage = "idle"
	}
	return status
}

func (s *DesktopService) InstallUpdate() bool {
	s.updateMu.Lock()
	if !s.updateStatus.Ready {
		s.updateMu.Unlock()
		return false
	}
	s.updateStatus.Stage = "installing"
	status := s.updateStatus
	s.updateMu.Unlock()
	s.publishUpdateStatus(status)

	s.mu.RLock()
	app := s.app
	s.mu.RUnlock()
	if app == nil || app.Updater.Restart(context.Background()) != nil {
		s.setUpdateStage("ready", true)
		return false
	}
	return true
}

func (s *DesktopService) ConsumeReleaseNotes() (*ReleaseNotes, error) {
	path := s.releaseNotesPath()
	var notes ReleaseNotes
	if err := readJSON(path, &notes); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	if notes.Version == "" {
		return nil, nil
	}
	if notes.Version == desktopVersion && strings.TrimSpace(notes.Notes) != "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return &notes, nil
	}
	if !newerVersion(notes.Version, desktopVersion) {
		_ = os.Remove(path)
	}
	return nil, nil
}

func (s *DesktopService) waitAndInstallMandatory() {
	nextReminder := time.Time{}
	for {
		if !s.prototype.editorIsOpen() {
			s.InstallUpdate()
			return
		}
		if time.Now().After(nextReminder) {
			s.setUpdateStage("waiting-editor", true)
			nextReminder = time.Now().Add(time.Minute)
		}
		time.Sleep(time.Second)
	}
}

func (s *DesktopService) setUpdateStage(stage string, ready bool) {
	s.updateMu.Lock()
	s.updateStatus.Stage = stage
	s.updateStatus.Ready = ready
	status := s.updateStatus
	s.updateMu.Unlock()
	s.publishUpdateStatus(status)
}

func (s *DesktopService) publishUpdateStatus(status UpdateStatus) {
	s.mu.RLock()
	app := s.app
	s.mu.RUnlock()
	if app != nil {
		app.Event.Emit("update:status", status)
	}
}

func (s *DesktopService) releaseNotesPath() string {
	return filepath.Join(s.paths.Root, "release-notes-wails.json")
}

func (s *DesktopService) pendingUpdatePath() string {
	return filepath.Join(s.paths.Root, "update-pending-wails.json")
}

func (s *DesktopService) markPendingUpdate(version string) {
	_ = writeJSONAtomic(s.pendingUpdatePath(), pendingUpdate{Version: version})
}

// consumePendingUpdate 读取并清除上次启动留下的“待安装”标记，返回它是否存在。
func (s *DesktopService) consumePendingUpdate() bool {
	path := s.pendingUpdatePath()
	var pending pendingUpdate
	if err := readJSON(path, &pending); err != nil || pending.Version == "" {
		return false
	}
	_ = os.Remove(path)
	return true
}

func releaseMandatory(release *updater.Release, current string) bool {
	if release == nil || release.Metadata == nil {
		return false
	}
	minimum, _ := release.Metadata["minVersion"].(string)
	return minimum != "" && newerVersion(minimum, current)
}

func newerVersion(candidate, current string) bool {
	parse := func(value string) [3]int {
		var result [3]int
		parts := strings.Split(strings.TrimPrefix(value, "v"), ".")
		for index := 0; index < len(result) && index < len(parts); index++ {
			number := strings.SplitN(parts[index], "-", 2)[0]
			result[index], _ = strconv.Atoi(number)
		}
		return result
	}
	left, right := parse(candidate), parse(current)
	for index := range left {
		if left[index] != right[index] {
			return left[index] > right[index]
		}
	}
	return false
}
