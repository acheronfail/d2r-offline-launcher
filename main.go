package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

//go:embed payload/license.so payload/LICENSE.fyne
var payload embed.FS

var verifyInstallBuild = verifyGameBuild
var launchStep = launch

type manifest struct {
	Game          string `json:"game"`
	Prefix        string `json:"prefix,omitempty"`
	Version       int    `json:"version"`
	ShortcutAppID string `json:"shortcut_app_id,omitempty"`
	LaunchOptions string `json:"original_launch_options,omitempty"`
}

func stateDir() string {
	if p := os.Getenv("D2R_OFFLINE_STATE_DIR"); p != "" {
		return p
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "share", "d2r-offline-launcher")
}
func validGame(p string) error {
	for _, n := range []string{"D2R.exe", "D2R_loader.dll"} {
		s, e := os.Stat(filepath.Join(p, n))
		if e != nil || s.IsDir() {
			return fmt.Errorf("Choose the folder containing %s", n)
		}
	}
	return nil
}
func autodetect() string {
	h, _ := os.UserHomeDir()
	for _, p := range []string{filepath.Join(h, "Games", "BattleNet", "Diablo II Resurrected"), filepath.Join(h, "Games", "Diablo II Resurrected")} {
		if validGame(p) == nil {
			return p
		}
	}
	return ""
}
func readManifest(state string) (manifest, error) {
	return readManifestFile(filepath.Join(state, "installation.json"))
}
func readSelection(state string) (manifest, error) {
	return readManifestFile(filepath.Join(state, "selection.json"))
}
func readManifestFile(path string) (manifest, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return manifest{}, e
	}
	var m manifest
	e = json.Unmarshal(b, &m)
	if e != nil || m.Version != 4 || m.Game == "" {
		return manifest{}, errors.New("Installation record is invalid")
	}
	return m, nil
}
func install(source string) error {
	return installWithPrefix(source, "")
}
func installWithPrefix(source, selectedPrefix string) error {
	if e := validGame(source); e != nil {
		return e
	}
	if e := verifyInstallBuild(source); e != nil {
		return e
	}
	source, e := filepath.Abs(source)
	if e != nil {
		return e
	}
	state := stateDir()
	if s, e := os.Lstat(state); e == nil && (!s.IsDir() || s.Mode()&os.ModeSymlink != 0) {
		return errors.New("Installation path is unsafe")
	}
	if e := os.MkdirAll(state, 0700); e != nil {
		return e
	}
	m := manifest{Game: source, Version: 4}
	if previous, e := readManifest(state); e == nil {
		if previous.Game != source {
			return errors.New("Uninstall the old game location first")
		}
		m = previous
	} else if !errors.Is(e, os.ErrNotExist) {
		return fmt.Errorf("Existing installation needs the earlier installer for removal: %w", e)
	} else {
		if saved, e := readSelection(state); e == nil && saved.Game == source {
			m = saved
		}
		if override := os.Getenv("D2R_OFFLINE_PREFIX"); override != "" {
			m.Prefix = override
		}
		if m.Prefix == "" {
			m.Prefix = shortcutPrefix(source)
		}
		if m.Prefix == "" {
			m.Prefix = choosePrefix()
		}
	}
	if selectedPrefix != "" {
		m.Prefix, e = filepath.Abs(selectedPrefix)
		if e != nil {
			return e
		}
	}
	if m.Prefix == "" {
		return errors.New("Could not find a Battle.net Proton prefix; set D2R_OFFLINE_PREFIX to its compatdata folder")
	}
	if s, e := os.Stat(filepath.Join(m.Prefix, "pfx")); e != nil || !s.IsDir() {
		return errors.New("Choose a Proton compatdata folder containing pfx")
	}
	if m.ShortcutAppID == "" {
		shortcut := shortcutForGame(source)
		m.ShortcutAppID = shortcut.AppID
		m.LaunchOptions = shortcut.LaunchOptions
	}
	licensePatch, e := payload.ReadFile("payload/license.so")
	if e != nil {
		return e
	}
	if e := os.WriteFile(filepath.Join(state, "license.so"), licensePatch, 0600); e != nil {
		return e
	}
	license, e := payload.ReadFile("payload/LICENSE.fyne")
	if e != nil {
		return e
	}
	if e := os.WriteFile(filepath.Join(state, "LICENSE.fyne"), license, 0644); e != nil {
		return e
	}
	if e := copyInstaller(source); e != nil {
		return e
	}
	if e := prepareTerrorZones(state, m); e != nil {
		return e
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if e := os.WriteFile(filepath.Join(state, "selection.json"), b, 0600); e != nil {
		return e
	}
	if e := os.WriteFile(filepath.Join(state, "installation.json"), b, 0600); e != nil {
		return e
	}
	return nil
}
func uninstall() error {
	state := stateDir()
	m, e := readManifest(state)
	if e != nil {
		return e
	}
	s, e := os.Lstat(state)
	if e != nil || !s.IsDir() || s.Mode()&os.ModeSymlink != 0 {
		return errors.New("Installation path is unsafe")
	}
	if e := restoreTerrorZones(state, m); e != nil {
		return e
	}
	for _, name := range []string{"license.so", terrorZoneBackupName, terrorZoneRecordName, "patch.log", "last-launch.log", "installation.json"} {
		if e := os.Remove(filepath.Join(state, name)); e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
	}
	return nil
}
func statusDetails() (string, string, string) {
	m, e := readManifest(stateDir())
	if e != nil {
		return "Not installed", "Choose the game folder and Proton prefix, then Install.", ""
	}
	_, anchor, e := offlineClockOffset(m.Prefix, time.Now())
	if e != nil {
		return "Installed", e.Error(), ""
	}
	return "Installed", "", anchor.Format("Mon 2 Jan 2006")
}
func inGamescope() bool {
	return strings.Contains(strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP")), "gamescope")
}
func newWindow(a fyne.App) fyne.Window {
	a.Settings().SetTheme(theme.DarkTheme())
	w := a.NewWindow("D2R Offline Launcher")
	w.Resize(fyne.NewSize(700, 300))
	if inGamescope() {
		w.SetFullScreen(true)
	}
	game := widget.NewEntry()
	game.TextStyle = fyne.TextStyle{Monospace: true}
	game.SetPlaceHolder("Choose the D2R folder")
	saved, e := readManifest(stateDir())
	if e != nil {
		saved, _ = readSelection(stateDir())
	}
	if saved.Game != "" {
		game.SetText(saved.Game)
	} else {
		game.SetText(autodetect())
	}
	prefix := widget.NewEntry()
	prefix.TextStyle = fyne.TextStyle{Monospace: true}
	prefix.SetPlaceHolder("Choose the Proton prefix")
	if saved.Prefix != "" {
		prefix.SetText(saved.Prefix)
	} else {
		prefix.SetText(choosePrefix())
	}
	title := widget.NewLabelWithStyle("D2R Offline Launcher", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.SizeName = theme.SizeNameHeadingText
	state := widget.NewLabelWithStyle("", fyne.TextAlignTrailing, fyne.TextStyle{Bold: true})
	state.SizeName = theme.SizeNameCaptionText
	r, g, b, _ := theme.Color(theme.ColorNameForeground).RGBA()
	captionColor := color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 175}
	caption := func(text string) *canvas.Text {
		label := canvas.NewText(text, captionColor)
		label.TextSize = theme.Size(theme.SizeNameCaptionText)
		label.TextStyle = fyne.TextStyle{Bold: true}
		return label
	}
	dateLabel := caption("OFFLINE GAME DATE")
	dateValue := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	dateValue.Importance = widget.HighImportance
	dateRow := container.NewBorder(nil, nil, dateLabel, nil, dateValue)
	note := widget.NewLabel("")
	note.Wrapping = fyne.TextWrapWord
	messageArea := container.NewVScroll(note)
	panel := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	panel.CornerRadius = 8
	status := container.NewStack(panel, container.NewPadded(container.NewBorder(dateRow, nil, nil, nil, messageArea)))
	setStatus := func(label, message, date string) {
		state.Importance = widget.LowImportance
		switch label {
		case "Installed":
			state.Importance = widget.SuccessImportance
		case "Working":
			state.Importance = widget.HighImportance
		case "Action needed":
			state.Importance = widget.DangerImportance
		}
		state.SetText(label)
		dateValue.SetText(date)
		if date == "" {
			dateRow.Hide()
		} else {
			dateRow.Show()
		}
		note.SetText(message)
		if message == "" {
			messageArea.Hide()
		} else {
			messageArea.Show()
		}
		status.Refresh()
	}
	var working atomic.Bool
	var actionButtons []*widget.Button
	var launchButton, uninstallButton *widget.Button
	syncButtons := func() {
		if _, e := readManifest(stateDir()); e != nil {
			launchButton.Disable()
			uninstallButton.Disable()
		}
	}
	refreshStatus := func() { setStatus(statusDetails()) }
	run := func(activity string, fn func() error) {
		if !working.CompareAndSwap(false, true) {
			return
		}
		for _, button := range actionButtons {
			button.Disable()
		}
		setStatus("Working", activity, "")
		go func() {
			e := fn()
			fyne.Do(func() {
				if e != nil {
					setStatus("Action needed", e.Error(), "")
					dialog.ShowError(e, w)
				} else {
					refreshStatus()
				}
				for _, button := range actionButtons {
					button.Enable()
				}
				syncButtons()
				working.Store(false)
			})
		}()
	}
	browse := widget.NewButtonWithIcon("Browse", theme.FolderOpenIcon(), func() {
		dialog.ShowFolderOpen(func(u fyne.ListableURI, e error) {
			if e != nil {
				dialog.ShowError(e, w)
			} else if u != nil {
				game.SetText(u.Path())
			}
		}, w)
	})
	browsePrefix := widget.NewButtonWithIcon("Browse", theme.FolderOpenIcon(), func() {
		dialog.ShowFolderOpen(func(u fyne.ListableURI, e error) {
			if e != nil {
				dialog.ShowError(e, w)
			} else if u != nil {
				prefix.SetText(u.Path())
			}
		}, w)
	})
	installButton := widget.NewButton("Install", func() {
		p, q := strings.TrimSpace(game.Text), strings.TrimSpace(prefix.Text)
		run("Installing…", func() error { return installWithPrefix(p, q) })
	})
	launchButton = widget.NewButton("Launch Offline", func() { run("Launching…", launchStep) })
	launchButton.Importance = widget.HighImportance
	uninstallButton = widget.NewButton("Uninstall", func() { run("Uninstalling…", uninstall) })
	exitButton := widget.NewButtonWithIcon("Exit", theme.CancelIcon(), w.Close)
	exitButton.Importance = widget.DangerImportance
	actionButtons = []*widget.Button{installButton, launchButton, uninstallButton, exitButton}
	pathField := func(label string, entry *widget.Entry, button *widget.Button) fyne.CanvasObject {
		return container.NewVBox(caption(label), container.NewBorder(nil, nil, nil, button, entry))
	}
	form := container.NewVBox(
		container.NewBorder(nil, nil, nil, state, title), widget.NewSeparator(),
		pathField("GAME FOLDER", game, browse),
		pathField("PROTON PREFIX", prefix, browsePrefix),
		container.NewGridWithColumns(2, launchButton, container.NewGridWithColumns(3, installButton, uninstallButton, exitButton)),
	)
	focusRing := canvas.NewRectangle(color.Transparent)
	focusRing.StrokeColor = color.NRGBA{R: 255, G: 205, B: 72, A: 255}
	focusRing.StrokeWidth = 3
	focusRing.CornerRadius = 8
	focusRing.Hide()
	ringLayer := container.NewWithoutLayout(focusRing)
	w.SetContent(container.NewStack(
		container.NewPadded(container.NewBorder(form, nil, nil, nil, status)),
		ringLayer,
	))
	refreshStatus()
	syncButtons()
	startGamepad(w, a, []gamepadTarget{
		{game, func() { browse.Tapped(nil) }},
		{browse, nil},
		{prefix, func() { browsePrefix.Tapped(nil) }},
		{browsePrefix, nil},
		{launchButton, nil},
		{installButton, nil},
		{uninstallButton, nil},
		{exitButton, nil},
	}, launchButton, installButton, focusRing, ringLayer)
	return w
}
func main() {
	if len(os.Args) > 1 {
		var err error
		switch os.Args[1] {
		case "--install":
			game := autodetect()
			if len(os.Args) > 2 {
				game = os.Args[2]
			}
			prefix := ""
			if len(os.Args) > 3 {
				prefix = os.Args[3]
			}
			err = installWithPrefix(game, prefix)
		case "--launch-offline":
			err = launch()
		case "--uninstall":
			err = uninstall()
		case "--offline-child":
			if len(os.Args) < 4 {
				err = errors.New("Missing offline runtime command")
			} else {
				err = offlineChild(os.Args[2], os.Args[3:])
			}
			reportOfflineChildError(err)
		default:
			fmt.Fprintln(os.Stderr, "Unknown command")
			os.Exit(2)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if inGamescope() && os.Getenv("FYNE_SCALE") == "" {
		os.Setenv("FYNE_SCALE", "0.6")
	}
	a := app.NewWithID("org.acheronfail.d2r-offline-launcher")
	newWindow(a).ShowAndRun()
}
