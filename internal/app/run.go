package app

import (
	"embed"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func init() {
	application.RegisterEvent[VideoInfo]("prototype:video")
	application.RegisterEvent[SwitchMetric]("prototype:switch")
	application.RegisterEvent[string]("prototype:request-close")
	application.RegisterEvent[FFmpegStatus]("ffmpeg:status")
	application.RegisterEvent[MediaProgress]("media:progress")
	application.RegisterEvent[[]LibraryEntry]("library:changed")
	application.RegisterEvent[ThumbReady]("thumb:ready")
	application.RegisterEvent[VideoReady]("video:ready")
	application.RegisterEvent[VideoFailed]("video:failed")
	application.RegisterEvent[HomeNotice]("home:refresh")
	application.RegisterEvent[[]string]("files:dropped")
	application.RegisterEvent[UpdateStatus]("update:status")
	application.RegisterEvent[map[string]string]("update:ready")
}

func Run(assets embed.FS) {
	cleanupReplacedExecutables()
	bundledAssets = assets
	startedAt := time.Now()
	prototype := newPrototypeService(startedAt)
	mediaServer, err := newLoopbackMediaServer(prototype.currentMediaPath)
	if err != nil {
		log.Fatal(err)
	}
	defer mediaServer.Close()
	prototype.attachMediaServer(mediaServer)
	paths, err := defaultAppPaths()
	if err != nil {
		log.Fatal(err)
	}
	var app *application.App
	ffmpeg := newFFmpegManager(paths, func(status FFmpegStatus) {
		if app != nil {
			app.Event.Emit("ffmpeg:status", status)
		}
	})
	desktop, err := newDesktopService(paths, ffmpeg, prototype)
	if err != nil {
		log.Fatal(err)
	}
	desktop.sweepPartFiles()
	formalID := os.Getenv("NONOKA_FORMAL_EDITOR_ID")
	mockID := formalID
	if mockID == "" {
		mockID = "pPbTHcdy3UR"
	}
	if desktop.config.TaskKey == "smoke-key" && os.Getenv("NONOKA_FORMAL_MOCK") == "1" {
		mockBackend, err := newFormalSmokeBackend(mockID)
		if err != nil {
			log.Fatal(err)
		}
		defer mockBackend.Close()
		desktop.mu.Lock()
		desktop.config.Backend = mockBackend.URL
		desktop.mu.Unlock()
	}
	staticAssets := application.AssetFileServerFS(assets)

	app = application.New(application.Options{
		Name:        "Nonoka Subtitle",
		Description: "Nonoka Subtitle",
		Services: []application.Service{
			application.NewService(prototype),
			application.NewService(desktop),
		},
		Assets: application.AssetOptions{
			Handler: staticAssets,
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		Windows: application.WindowsOptions{
			WebviewUserDataPath: paths.WebviewDir,
		},
	})
	desktop.mu.Lock()
	desktop.app = app
	desktop.mu.Unlock()
	prototype.attachWindowState(desktop.applyWindowOptions, desktop.trackWindowState)
	if err := desktop.initAutoUpdate(app); err != nil {
		log.Printf("auto update: %v", err)
	}

	homeOptions := desktop.applyWindowOptions("home", application.WebviewWindowOptions{
		Name:             "home",
		Title:            "Nonoka Subtitle",
		Width:            1180,
		Height:           760,
		MinWidth:         960,
		MinHeight:        620,
		InitialPosition:  application.WindowCentered,
		BackgroundColour: application.NewRGB(14, 17, 24),
		URL:              "/index.html",
		EnableFileDrop:   true,
		Frameless:        runtime.GOOS == "windows",
		Windows: application.WindowsWindow{
			Theme:                  application.Dark,
			NonClientRegionSupport: true,
		},
	})
	homeOptions, deferredHomeState := deferRestoredWindowStartState(homeOptions)
	home := app.Window.NewWithOptions(homeOptions)
	if deferredHomeState != application.WindowStateNormal {
		home.OnWindowEvent(events.Common.WindowRuntimeReady, func(_ *application.WindowEvent) {
			showWithDeferredWindowState(home, deferredHomeState)
		})
	}
	desktop.trackWindowState("home", home)
	home.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		app.Event.Emit("files:dropped", event.Context().DroppedFiles())
	})

	prototype.attach(app, home)
	desktop.attach(app, home)
	desktop.startAutoUpdate()
	if os.Getenv("NONOKA_SKIP_FFMPEG") != "1" {
		ffmpeg.Start()
	}
	if mediaPath := os.Getenv("NONOKA_PROTOTYPE_MEDIA"); mediaPath != "" {
		if _, err := prototype.loadVideo(mediaPath); err != nil {
			log.Printf("prototype media: %v", err)
		}
	}
	home.RegisterHook(events.Common.WindowClosing, func(_ *application.WindowEvent) {
		prototype.prepareQuit()
	})
	if formalID != "" {
		go func() {
			time.Sleep(1500 * time.Millisecond)
			if _, err := desktop.OpenLibraryVideo(formalID); err != nil {
				log.Printf("formal editor smoke: %v", err)
			}
			time.Sleep(10 * time.Second)
			prototype.closeEditorForSmoke()
			time.Sleep(750 * time.Millisecond)
			if resultPath := os.Getenv("NONOKA_WINDOW_RESULT"); resultPath != "" {
				if err := prototype.writeWindowProbe(resultPath); err != nil {
					log.Printf("window probe: %v", err)
				}
			}
		}()
	} else if os.Getenv("NONOKA_PROTOTYPE_SMOKE") == "1" {
		go func() {
			time.Sleep(1500 * time.Millisecond)
			prototype.OpenEditor()
			time.Sleep(3500 * time.Millisecond)
			prototype.closeEditorForSmoke()
			time.Sleep(750 * time.Millisecond)
			if resultPath := os.Getenv("NONOKA_WINDOW_RESULT"); resultPath != "" {
				if err := prototype.writeWindowProbe(resultPath); err != nil {
					log.Printf("window probe: %v", err)
				}
			}
		}()
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
