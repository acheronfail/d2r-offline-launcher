package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

//go:embed payload/license.so payload/LICENSE.fyne
var payload embed.FS

var verifyInstallBuild = verifyGameBuild
var launchStep = launch

type manifest struct {
	Game                  string `json:"game"`
	Prefix                string `json:"prefix,omitempty"`
	Version               int    `json:"version"`
	ShortcutFile          string `json:"shortcut_file,omitempty"`
	ShortcutAppID         string `json:"shortcut_app_id,omitempty"`
	ShortcutName          string `json:"shortcut_name,omitempty"`
	OriginalExe           string `json:"original_exe,omitempty"`
	OriginalStartDir      string `json:"original_start_dir,omitempty"`
	OriginalLaunchOptions string `json:"original_launch_options,omitempty"`
	CompatFile            string `json:"compat_file,omitempty"`
	OriginalCompatBlock   string `json:"original_compat_block,omitempty"`
}

func stateDir() string {
	if p := os.Getenv("D2R_CAMPING_STATE_DIR"); p != "" {
		return p
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "share", "d2r-camping")
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
	b, e := os.ReadFile(filepath.Join(state, "installation.json"))
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
		m.Prefix = os.Getenv("D2R_CAMPING_PREFIX")
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
		return errors.New("Could not find a Battle.net Proton prefix; set D2R_CAMPING_PREFIX to its compatdata folder")
	}
	if s, e := os.Stat(filepath.Join(m.Prefix, "pfx")); e != nil || !s.IsDir() {
		return errors.New("Choose a Proton compatdata folder containing pfx")
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
	if e := prepareShortcutInstall(state, &m); e != nil {
		return e
	}
	if e := prepareTerrorZones(state, m); e != nil {
		return e
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if e := os.WriteFile(filepath.Join(state, "installation.json"), b, 0600); e != nil {
		return e
	}
	return applyOrScheduleShortcut(state, m)
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
	queued, e := restoreOrScheduleShortcut(state, m)
	if e != nil {
		return e
	}
	if queued {
		return nil
	}
	return os.RemoveAll(state)
}
func description() string {
	if failure, e := os.ReadFile(filepath.Join(stateDir(), "steam-transition-error.txt")); e == nil {
		return "Steam shortcut change failed: " + string(failure)
	}
	if action, e := os.ReadFile(filepath.Join(stateDir(), "steam-transition-pending")); e == nil {
		return "Applying " + string(action) + " after Steam closes. Reopen Steam when it finishes."
	}
	m, e := readManifest(stateDir())
	if e != nil {
		return "Choose your Battle.net D2R folder, then tap Install."
	}
	return "Installed: " + m.Game + "\nProton prefix: " + m.Prefix + offlineClockStatus(m)
}
func newWindow(a fyne.App) fyne.Window {
	w := a.NewWindow("D2R Camping Installer")
	w.Resize(fyne.NewSize(640, 340))
	game := widget.NewEntry()
	game.SetPlaceHolder("Diablo II Resurrected folder")
	if installed, e := readManifest(stateDir()); e == nil {
		game.SetText(installed.Game)
	} else {
		game.SetText(autodetect())
	}
	prefix := widget.NewEntry()
	prefix.SetPlaceHolder("Battle.net Proton compatdata folder")
	if installed, e := readManifest(stateDir()); e == nil {
		prefix.SetText(installed.Prefix)
	} else {
		prefix.SetText(choosePrefix())
	}
	status := widget.NewLabel(description())
	status.Wrapping = fyne.TextWrapWord
	busy := widget.NewProgressBarInfinite()
	busy.Hide()
	var working atomic.Bool
	var actionButtons []*widget.Button
	run := func(fn func() error) {
		if !working.CompareAndSwap(false, true) {
			return
		}
		for _, button := range actionButtons {
			button.Disable()
		}
		status.SetText("Working…")
		busy.Show()
		go func() {
			e := fn()
			fyne.Do(func() {
				busy.Hide()
				if e != nil {
					status.SetText(e.Error())
					dialog.ShowError(e, w)
				} else {
					status.SetText(description())
				}
				for _, button := range actionButtons {
					button.Enable()
				}
				working.Store(false)
			})
		}()
	}
	browse := widget.NewButton("Browse…", func() {
		dialog.ShowFolderOpen(func(u fyne.ListableURI, e error) {
			if e != nil {
				dialog.ShowError(e, w)
			} else if u != nil {
				game.SetText(u.Path())
			}
		}, w)
	})
	browsePrefix := widget.NewButton("Browse…", func() {
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
		run(func() error { return installWithPrefix(p, q) })
	})
	launchButton := widget.NewButton("Launch Offline", func() { run(launchStep) })
	uninstallButton := widget.NewButton("Uninstall", func() { run(uninstall) })
	actionButtons = []*widget.Button{installButton, launchButton, uninstallButton}
	w.SetContent(container.NewVBox(widget.NewLabel("D2R Camping"), widget.NewLabel("Use your cached Battle.net license offline. Online play stays available."), container.NewBorder(nil, nil, nil, browse, game), container.NewBorder(nil, nil, nil, browsePrefix, prefix), container.NewGridWithColumns(3, installButton, launchButton, uninstallButton), busy, status))
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
		case "--finish-steam-transition":
			if len(os.Args) != 3 {
				err = errors.New("Missing Steam transition action")
			} else {
				err = finishSteamTransition(os.Args[2])
			}
			recordSteamTransitionFailure(err)
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
	a := app.NewWithID("org.acheronfail.d2r-camping")
	newWindow(a).ShowAndRun()
}
