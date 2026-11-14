package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

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

func TestGUIInstallAndUninstallButtons(t *testing.T) {
	allowFixtureBuild(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	game := filepath.Join(home, "Games", "BattleNet", "Diablo II Resurrected")
	if err := os.MkdirAll(game, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"D2R.exe", "D2R_loader.dll"} {
		if err := os.WriteFile(filepath.Join(game, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	prefix := filepath.Join(home, "compatdata", "12345")
	if err := os.MkdirAll(filepath.Join(prefix, "pfx"), 0700); err != nil {
		t.Fatal(err)
	}
	setupSteamShortcut(t, home, game, prefix)
	state := filepath.Join(home, "installed")
	t.Setenv("D2R_CAMPING_PREFIX", prefix)
	t.Setenv("D2R_CAMPING_STATE_DIR", state)
	a := test.NewApp()
	defer a.Quit()
	w := newWindow(a)
	installButton := findButton(w.Content(), "Install")
	launchButton := findButton(w.Content(), "Launch Offline")
	uninstallButton := findButton(w.Content(), "Uninstall")
	if installButton == nil || launchButton == nil || uninstallButton == nil {
		t.Fatal("GUI is missing Install, Launch Offline, or Uninstall")
	}
	test.Tap(installButton)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(state, "installation.json")); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	manifest, err := readManifest(state)
	if err != nil || manifest.Prefix != prefix || manifest.Game != game {
		t.Fatalf("GUI Install did not save the selected paths: %+v, %v", manifest, err)
	}
	installed, err := os.ReadFile(filepath.Join(state, "license.so"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := payload.ReadFile("payload/license.so")
	if err != nil || !bytes.Equal(installed, want) {
		t.Fatal("GUI Install did not extract the embedded license library")
	}
	for installButton.Disabled() && time.Now().Before(deadline.Add(5*time.Second)) {
		time.Sleep(10 * time.Millisecond)
	}
	called := make(chan struct{}, 1)
	launchStep = func() error { called <- struct{}{}; return nil }
	defer func() { launchStep = launch }()
	test.Tap(launchButton)
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("GUI Launch Offline button did not start the launch action")
	}
	for launchButton.Disabled() && time.Now().Before(deadline.Add(5*time.Second)) {
		time.Sleep(10 * time.Millisecond)
	}
	test.Tap(uninstallButton)
	for time.Now().Before(deadline.Add(5 * time.Second)) {
		if _, err := os.Stat(state); os.IsNotExist(err) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatal("GUI Uninstall did not remove the temporary installation")
	}
	if data, err := os.ReadFile(filepath.Join(game, "D2R.exe")); err != nil || !bytes.Equal(data, []byte("D2R.exe")) {
		t.Fatal("GUI actions changed the original game")
	}
}
