package app

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const wailsVersion = "v3.0.0-alpha2.119"

var errUnsupportedVideo = errors.New("请选择 MP4、MOV、MKV、M4V 或 WebM 视频")

type VideoInfo struct {
	Available  bool   `json:"available"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modifiedAt"`
	URL        string `json:"url"`
}

type RuntimeInfo struct {
	GoVersion    string `json:"goVersion"`
	WailsVersion string `json:"wailsVersion"`
	Platform     string `json:"platform"`
	ProcessID    int    `json:"processId"`
	StartedAt    int64  `json:"startedAt"`
	UptimeMillis int64  `json:"uptimeMillis"`
	SmokeMode    bool   `json:"smokeMode"`
}

type SwitchMetric struct {
	Kind     string `json:"kind"`
	Millis   int64  `json:"millis"`
	Sequence uint64 `json:"sequence"`
}

type PlaybackProbe struct {
	Ready       bool    `json:"ready"`
	Seeked      bool    `json:"seeked"`
	Duration    float64 `json:"duration"`
	CurrentTime float64 `json:"currentTime"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	Error       string  `json:"error"`
}

type WindowProbe struct {
	HomeVisible   bool `json:"homeVisible"`
	EditorVisible bool `json:"editorVisible"`
	Responsive    bool `json:"responsive"`
}

type HomeProbe struct {
	Ready bool   `json:"ready"`
	State string `json:"state"`
	Cards int    `json:"cards"`
	Error string `json:"error"`
}

type HomeNotice struct {
	Error        string `json:"error"`
	Unauthorized bool   `json:"unauthorized"`
}

type PrototypeService struct {
	mu            sync.RWMutex
	app           *application.App
	home          *application.WebviewWindow
	editor        *application.WebviewWindow
	mediaPath     string
	video         VideoInfo
	playback      PlaybackProbe
	dirty         bool
	quitting      bool
	formal        bool
	closing       bool
	editorID      string
	homeNotice    HomeNotice
	sequence      uint64
	resourceSeq   uint64
	startedAt     time.Time
	switchStart   time.Time
	mediaURL      func(uint64, string) string
	windowOptions func(string, application.WebviewWindowOptions) application.WebviewWindowOptions
	trackWindow   func(string, *application.WebviewWindow)
}

func newPrototypeService(startedAt time.Time) *PrototypeService {
	return &PrototypeService{startedAt: startedAt}
}

func (p *PrototypeService) attach(app *application.App, home *application.WebviewWindow) {
	p.mu.Lock()
	p.app = app
	p.home = home
	p.mu.Unlock()
}

func (p *PrototypeService) attachMediaServer(server *loopbackMediaServer) {
	p.mu.Lock()
	p.mediaURL = server.RegisterURL
	p.mu.Unlock()
}

func (p *PrototypeService) attachWindowState(
	options func(string, application.WebviewWindowOptions) application.WebviewWindowOptions,
	track func(string, *application.WebviewWindow),
) {
	p.mu.Lock()
	p.windowOptions = options
	p.trackWindow = track
	p.mu.Unlock()
}

func (p *PrototypeService) currentMediaPath() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.mediaPath
}

func (p *PrototypeService) localFileURL(path string) (string, error) {
	p.mu.Lock()
	p.resourceSeq++
	sequence, makeURL := p.resourceSeq, p.mediaURL
	p.mu.Unlock()
	if makeURL == nil || !fileExists(path) {
		return "", errors.New("本地资源不存在")
	}
	return makeURL(sequence, path), nil
}

func (p *PrototypeService) dialogWindow() *application.WebviewWindow {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.editor != nil {
		return p.editor
	}
	return p.home
}

func (p *PrototypeService) editorIsOpen() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.editor != nil
}

func (p *PrototypeService) RuntimeInfo() RuntimeInfo {
	return RuntimeInfo{
		GoVersion:    runtime.Version(),
		WailsVersion: wailsVersion,
		Platform:     runtime.GOOS + "/" + runtime.GOARCH,
		ProcessID:    os.Getpid(),
		StartedAt:    p.startedAt.UnixMilli(),
		UptimeMillis: time.Since(p.startedAt).Milliseconds(),
		SmokeMode:    os.Getenv("NONOKA_PROTOTYPE_SMOKE") == "1" || os.Getenv("NONOKA_FORMAL_EDITOR_ID") != "",
	}
}

func (p *PrototypeService) Ping(value int) int {
	return value
}

func (p *PrototypeService) CurrentVideo() VideoInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.video
}

func (p *PrototypeService) PlaybackStatus() PlaybackProbe {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.playback
}

func (p *PrototypeService) ReportPlaybackProbe(probe PlaybackProbe) error {
	p.mu.Lock()
	p.playback = probe
	p.mu.Unlock()
	resultPath := os.Getenv("NONOKA_PROTOTYPE_RESULT")
	if resultPath == "" {
		return nil
	}
	data, err := json.MarshalIndent(probe, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(resultPath, data, 0o600)
}

func (p *PrototypeService) ReportHomeProbe(probe HomeProbe) error {
	resultPath := os.Getenv("NONOKA_HOME_RESULT")
	if resultPath == "" {
		return nil
	}
	data, err := json.MarshalIndent(probe, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(resultPath, data, 0o600)
}

func (p *PrototypeService) SelectVideoAndOpen() (VideoInfo, error) {
	p.mu.RLock()
	app, home := p.app, p.home
	p.mu.RUnlock()
	if app == nil || home == nil {
		return VideoInfo{}, errors.New("应用尚未初始化")
	}

	path, err := app.Dialog.OpenFile().
		SetTitle("选择原型测试视频").
		AttachToWindow(home).
		AddFilter("视频文件", "*.mp4;*.m4v;*.mov;*.mkv;*.webm").
		PromptForSingleSelection()
	if err != nil || path == "" {
		return VideoInfo{}, err
	}

	info, err := p.loadVideo(path)
	if err != nil {
		return VideoInfo{}, err
	}

	p.openEditor()
	app.Event.Emit("prototype:video", info)
	return info, nil
}

func (p *PrototypeService) loadVideo(path string) (VideoInfo, error) {
	info, err := videoInfo(path)
	if err != nil {
		return VideoInfo{}, err
	}
	p.mu.Lock()
	if p.mediaURL == nil {
		p.mu.Unlock()
		return VideoInfo{}, errors.New("media server unavailable")
	}
	p.mediaPath = path
	p.sequence++
	info.URL = p.mediaURL(p.sequence, path)
	p.video = info
	p.playback = PlaybackProbe{}
	p.mu.Unlock()
	return info, nil
}

func (p *PrototypeService) OpenEditor() {
	p.openEditor()
}

func (p *PrototypeService) openFormalEditor(id string) {
	p.mu.Lock()
	p.formal = true
	p.closing = false
	p.editorID = id
	p.mu.Unlock()
	p.openEditor()
}

func (p *PrototypeService) SetDirty(dirty bool) {
	p.mu.Lock()
	p.dirty = dirty
	p.mu.Unlock()
}

func (p *PrototypeService) ReturnHome() {
	p.mu.Lock()
	p.dirty = false
	p.closing = true
	editor, home := p.editor, p.home
	p.mu.Unlock()
	if editor != nil {
		editor.Close()
		return
	}
	p.mu.Lock()
	p.closing = false
	p.mu.Unlock()
	if home != nil {
		home.Show()
		home.Focus()
	}
}

func (p *PrototypeService) CloseEditor(notice HomeNotice) {
	p.mu.Lock()
	p.homeNotice = notice
	p.mu.Unlock()
	p.ReturnHome()
}

func (p *PrototypeService) DiscardAndReturn() {
	p.ReturnHome()
}

func (p *PrototypeService) openEditor() {
	p.mu.Lock()
	if p.app == nil || p.home == nil || p.quitting {
		p.mu.Unlock()
		return
	}
	p.sequence++
	sequence := p.sequence
	p.switchStart = time.Now()
	app, home, editor := p.app, p.home, p.editor
	editorURL := "/editor.html"
	editorTitle := "NONOKA SUBTITLE - Editor Prototype"
	if p.formal && p.editorID != "" {
		editorURL = "/editor.html?v=" + url.QueryEscape(p.editorID)
		editorTitle = "Nonoka Subtitle - Editor"
	}
	if editor == nil {
		options := application.WebviewWindowOptions{
			Name:             "editor",
			Title:            editorTitle,
			Width:            1440,
			Height:           900,
			MinWidth:         1024,
			MinHeight:        640,
			InitialPosition:  application.WindowCentered,
			StartState:       application.WindowStateMaximised,
			Hidden:           true,
			BackgroundColour: application.NewRGB(10, 13, 19),
			URL:              editorURL,
			Frameless:        runtime.GOOS == "windows",
			Windows: application.WindowsWindow{
				Theme:                      application.Dark,
				NonClientRegionSupport:     true,
				WebView2CompositionHosting: true,
			},
		}
		if p.windowOptions != nil {
			options = p.windowOptions("editor", options)
		}
		options, deferredStartState := deferRestoredWindowStartState(options)
		editor = app.Window.NewWithOptions(options)
		p.editor = editor
		p.installEditorHooks(editor)
		if p.trackWindow != nil {
			p.trackWindow("editor", editor)
		}
		p.mu.Unlock()

		editor.OnWindowEvent(events.Common.WindowRuntimeReady, func(_ *application.WindowEvent) {
			showWithDeferredWindowState(editor, deferredStartState)
			home.Hide()
			app.Event.Emit("prototype:switch", SwitchMetric{
				Kind:     "first-open",
				Millis:   time.Since(p.switchStart).Milliseconds(),
				Sequence: sequence,
			})
		})
		return
	}
	p.mu.Unlock()

	editor.Show()
	editor.Focus()
	home.Hide()
	app.Event.Emit("prototype:switch", SwitchMetric{
		Kind:     "reuse",
		Millis:   time.Since(p.switchStart).Milliseconds(),
		Sequence: sequence,
	})
}

func (p *PrototypeService) installEditorHooks(editor *application.WebviewWindow) {
	editor.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		p.mu.RLock()
		dirty, formal, closing, quitting, app := p.dirty, p.formal, p.closing, p.quitting, p.app
		p.mu.RUnlock()
		if quitting {
			return
		}
		if closing {
			p.finishEditorClose(editor)
			return
		}
		if formal || dirty {
			event.Cancel()
			app.Event.Emit("prototype:request-close", "窗口关闭被 Go 拦截")
			return
		}
		p.finishEditorClose(editor)
	})
}

func (p *PrototypeService) finishEditorClose(editor *application.WebviewWindow) {
	p.mu.Lock()
	if p.editor != editor {
		p.mu.Unlock()
		return
	}
	p.editor = nil
	p.dirty = false
	p.formal = false
	p.closing = false
	p.editorID = ""
	home := p.home
	app := p.app
	notice := p.homeNotice
	p.homeNotice = HomeNotice{}
	p.mu.Unlock()
	if home != nil {
		home.Show()
		home.Focus()
	}
	if app != nil {
		app.Event.Emit("home:refresh", notice)
	}
}

func (p *PrototypeService) closeEditorForSmoke() {
	p.mu.RLock()
	editor := p.editor
	p.mu.RUnlock()
	if editor != nil {
		editor.Close()
	}
}

func (p *PrototypeService) writeWindowProbe(path string) error {
	p.mu.RLock()
	home, editor := p.home, p.editor
	p.mu.RUnlock()
	probe := WindowProbe{Responsive: true}
	if home != nil {
		probe.HomeVisible = home.IsVisible()
	}
	if editor != nil {
		probe.EditorVisible = editor.IsVisible()
	}
	data, err := json.MarshalIndent(probe, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (p *PrototypeService) prepareQuit() {
	p.mu.Lock()
	p.quitting = true
	editor := p.editor
	p.mu.Unlock()
	if editor != nil {
		go editor.Close()
	}
}

func videoInfo(path string) (VideoInfo, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp4", ".m4v", ".mov", ".mkv", ".webm":
	default:
		return VideoInfo{}, errUnsupportedVideo
	}
	stat, err := os.Stat(path)
	if err != nil {
		return VideoInfo{}, err
	}
	if stat.IsDir() {
		return VideoInfo{}, errUnsupportedVideo
	}
	return VideoInfo{
		Available:  true,
		Name:       stat.Name(),
		Size:       stat.Size(),
		ModifiedAt: stat.ModTime().Format(time.RFC3339),
	}, nil
}
