package app

import (
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const windowStateDebounce = 400 * time.Millisecond

func deferRestoredWindowStartState(options application.WebviewWindowOptions) (application.WebviewWindowOptions, application.WindowState) {
	deferred := application.WindowStateNormal
	if options.StartState == application.WindowStateMaximised || options.StartState == application.WindowStateFullscreen {
		deferred = options.StartState
		options.Hidden = true
		options.StartState = application.WindowStateNormal
	}
	return options, deferred
}

func showWithDeferredWindowState(window *application.WebviewWindow, state application.WindowState) {
	switch state {
	case application.WindowStateMaximised:
		window.Maximise()
	case application.WindowStateFullscreen:
		window.Fullscreen()
	}
	window.Show()
}

func validWindowBounds(bounds WindowBounds, minWidth, minHeight int) bool {
	return bounds.Width >= minWidth && bounds.Height >= minHeight &&
		bounds.Width <= 16384 && bounds.Height <= 16384 &&
		bounds.X >= -65536 && bounds.X <= 65536 && bounds.Y >= -65536 && bounds.Y <= 65536
}

func windowBoundsOnScreens(bounds WindowBounds, screens []*application.Screen) bool {
	if len(screens) == 0 {
		return true
	}
	for _, screen := range screens {
		if screen == nil {
			continue
		}
		area := screen.WorkArea
		left := max(bounds.X, area.X)
		top := max(bounds.Y, area.Y)
		right := min(bounds.X+bounds.Width, area.X+area.Width)
		bottom := min(bounds.Y+bounds.Height, area.Y+area.Height)
		if right-left >= 64 && bottom-top >= 64 {
			return true
		}
	}
	return false
}

func (s *DesktopService) applyWindowOptions(kind string, options application.WebviewWindowOptions) application.WebviewWindowOptions {
	s.mu.RLock()
	bounds, ok := s.config.Bounds[kind]
	s.mu.RUnlock()
	if !ok || !validWindowBounds(bounds, options.MinWidth, options.MinHeight) {
		return options
	}
	options.Width = bounds.Width
	options.Height = bounds.Height
	options.X = bounds.X
	options.Y = bounds.Y
	options.InitialPosition = application.WindowXY
	options.StartState = application.WindowStateNormal
	if bounds.FullScreen {
		options.StartState = application.WindowStateFullscreen
	} else if bounds.Maximized {
		options.StartState = application.WindowStateMaximised
	}
	return options
}

func (s *DesktopService) trackWindowState(kind string, window *application.WebviewWindow) {
	s.mu.RLock()
	normal := s.config.Bounds[kind]
	app := s.app
	s.mu.RUnlock()
	var trackerMu sync.Mutex
	var timer *time.Timer

	capture := func(persist bool) {
		trackerMu.Lock()
		if timer != nil {
			timer.Stop()
			timer = nil
		}
		maximized := window.IsMaximised()
		fullscreen := window.IsFullscreen()
		if !maximized && !fullscreen {
			normal.X, normal.Y = window.Position()
			normal.Width, normal.Height = window.Size()
		}
		bounds := normal
		bounds.Maximized = maximized
		bounds.FullScreen = fullscreen
		trackerMu.Unlock()
		if persist && validWindowBounds(bounds, 200, 160) {
			s.persistWindowBounds(kind, bounds)
		}
	}
	schedule := func(_ *application.WindowEvent) {
		trackerMu.Lock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(windowStateDebounce, func() { capture(true) })
		trackerMu.Unlock()
	}

	window.OnWindowEvent(events.Common.WindowRuntimeReady, func(_ *application.WindowEvent) {
		time.AfterFunc(150*time.Millisecond, func() {
			if app != nil {
				bounds := WindowBounds{}
				bounds.X, bounds.Y = window.Position()
				bounds.Width, bounds.Height = window.Size()
				if validWindowBounds(bounds, 200, 160) && !windowBoundsOnScreens(bounds, app.Screen.GetAll()) {
					if primary := app.Screen.GetPrimary(); primary != nil {
						area := primary.WorkArea
						window.SetPosition(area.X+(area.Width-bounds.Width)/2, area.Y+(area.Height-bounds.Height)/2)
					}
				}
			}
			capture(true)
		})
	})
	for _, eventType := range []events.WindowEventType{
		events.Common.WindowDidMove,
		events.Common.WindowDidResize,
		events.Common.WindowMaximise,
		events.Common.WindowUnMaximise,
		events.Common.WindowFullscreen,
		events.Common.WindowUnFullscreen,
	} {
		window.OnWindowEvent(eventType, schedule)
	}
	window.OnWindowEvent(events.Common.WindowClosing, func(_ *application.WindowEvent) {
		trackerMu.Lock()
		if timer != nil {
			timer.Stop()
			timer = nil
		}
		trackerMu.Unlock()
	})
}

func (s *DesktopService) persistWindowBounds(kind string, bounds WindowBounds) {
	if kind != "home" && kind != "editor" {
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.mu.Lock()
	if s.config.Bounds == nil {
		s.config.Bounds = make(map[string]WindowBounds)
	}
	previous, hadPrevious := s.config.Bounds[kind]
	s.config.Bounds[kind] = bounds
	config := persistedConfig(s.config)
	if err := writeJSONAtomic(s.paths.ConfigFile, config); err != nil {
		if hadPrevious {
			s.config.Bounds[kind] = previous
		} else {
			delete(s.config.Bounds, kind)
		}
	}
	s.mu.Unlock()
}
