package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestGUIExitLeavesSettingsIntact(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	state := filepath.Join(home, "state")
	t.Setenv("D2R_OFFLINE_STATE_DIR", state)
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	record, err := json.Marshal(manifest{Game: filepath.Join(home, "game"), Prefix: filepath.Join(home, "prefix"), Version: 4})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "settings.json"), record, 0600); err != nil {
		t.Fatal(err)
	}
	a := test.NewApp()
	defer a.Quit()
	w := newWindow(a)
	w.Show()
	exit := findButton(w.Content(), "Exit")
	if exit == nil {
		t.Fatal("Exit button is missing")
	}
	position := a.Driver().AbsolutePositionForObject(exit)
	test.TapCanvas(w.Canvas(), fyne.NewPos(position.X+exit.Size().Width/2, position.Y+exit.Size().Height/2))
	for _, open := range a.Driver().AllWindows() {
		if open == w {
			t.Fatal("Exit did not close the launcher window")
		}
	}
	if _, err := readManifest(state); err != nil {
		t.Fatalf("Exit changed saved settings: %v", err)
	}
}

func findButton(object fyne.CanvasObject, label string) *widget.Button {
	if button, ok := object.(*widget.Button); ok && button.Text == label {
		return button
	}
	if box, ok := object.(*fyne.Container); ok {
		for _, child := range box.Objects {
			if button := findButton(child, label); button != nil {
				return button
			}
		}
	}
	return nil
}

func findStatusPanel(object fyne.CanvasObject) *canvas.Rectangle {
	if panel, ok := object.(*canvas.Rectangle); ok && panel.CornerRadius == 8 {
		return panel
	}
	if box, ok := object.(*fyne.Container); ok {
		for _, child := range box.Objects {
			if panel := findStatusPanel(child); panel != nil {
				return panel
			}
		}
	}
	return nil
}

type interceptWindow struct {
	fyne.Window
	closeRequested func()
}

func (w *interceptWindow) SetCloseIntercept(fn func()) {
	w.closeRequested = fn
	w.Window.SetCloseIntercept(fn)
}

// The headless Fyne driver runs Do callbacks on the calling goroutine. Serialize
// them here to model the native event queue, including assertions after cleanup.
type serializedTestDriver struct {
	fyne.Driver
	mu sync.Mutex
}

func (d *serializedTestDriver) DoFromGoroutine(fn func(), _ bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fn()
}

type interceptApp struct {
	fyne.App
	window *interceptWindow
	driver *serializedTestDriver
}

func (a *interceptApp) Driver() fyne.Driver { return a.driver }

func (a *interceptApp) NewWindow(title string) fyne.Window {
	a.window = &interceptWindow{Window: a.App.NewWindow(title)}
	return a.window
}

func TestGUISessionLifecycle(t *testing.T) {
	allowFixtureBuild(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	game, prefix := fixtureSession(t)
	t.Setenv("D2R_OFFLINE_PREFIX", prefix)
	// Start with saved paths, not an installed marker.
	if err := saveSettings(game, prefix); err != nil {
		t.Fatal(err)
	}
	started, stop, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	launchStep = func(ready func()) error { ready(); close(started); <-stop; return nil }
	defer func() { launchStep = launch }()
	base := test.NewApp()
	a := &interceptApp{App: base, driver: &serializedTestDriver{Driver: base.Driver()}}
	fyne.SetCurrentApp(a)
	defer a.Quit()
	w := newWindow(a)
	w.Show()
	defer func() { w.SetCloseIntercept(nil); w.Close() }()
	if findButton(w.Content(), "Install") != nil || findButton(w.Content(), "Uninstall") != nil {
		t.Fatal("Install controls remain")
	}
	panel := findStatusPanel(w.Content())
	if panel == nil || panel.Size().Height < 40 {
		t.Fatal("Missing or clipped status panel")
	}
	panelPosition, panelSize := a.Driver().AbsolutePositionForObject(panel), panel.Size()
	checkLayout := func() {
		if a.Driver().AbsolutePositionForObject(panel) != panelPosition || panel.Size() != panelSize {
			t.Error("Status panel moved during session")
		}
		if panelPosition.Y+panelSize.Height > w.Canvas().Size().Height {
			t.Error("Status panel clips below window")
		}
	}
	launchButton := findButton(w.Content(), "Launch Offline")
	exit := findButton(w.Content(), "Exit")
	if launchButton == nil || exit == nil {
		t.Fatal("Missing session controls")
	}
	test.Tap(launchButton)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("Launch did not start")
	}
	checkLayout()
	if !launchButton.Disabled() {
		t.Fatal("Launch enabled during game")
	}
	a.window.closeRequested() // Simulate the native window manager close event.
	if w.Canvas() == nil {
		t.Fatal("Window was destroyed")
	}
	close(stop)
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			enabled := false
			fyne.DoAndWait(func() { enabled = !launchButton.Disabled() })
			if enabled {
				close(finished)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	select {
	case <-finished:
	case <-time.After(6 * time.Second):
		t.Fatal("Controls did not recover after game exit")
	}
	checkLayout()
	if _, err := readManifest(stateDir()); err != nil {
		t.Fatal(err)
	}
}
