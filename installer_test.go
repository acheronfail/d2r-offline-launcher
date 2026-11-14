package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func allowFixtureBuild(t *testing.T) {
	previous := verifyInstallBuild
	verifyInstallBuild = func(string) error { return nil }
	t.Cleanup(func() { verifyInstallBuild = previous })
}

func TestInstallAndUninstallLeaveGameAndCookieUntouched(t *testing.T) {
	allowFixtureBuild(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	game := filepath.Join(home, "game")
	if e := os.MkdirAll(filepath.Join(game, "Data"), 0700); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"D2R.exe", "D2R_loader.dll"} {
		if e := os.WriteFile(filepath.Join(game, name), []byte(name), 0600); e != nil {
			t.Fatal(e)
		}
	}
	prefix := filepath.Join(home, ".steam", "steam", "steamapps", "compatdata", "12345")
	cookie := cookiePath(prefix)
	if e := os.MkdirAll(filepath.Dir(cookie), 0700); e != nil {
		t.Fatal(e)
	}
	original := []byte("signed-cookie-fixture")
	if e := os.WriteFile(cookie, original, 0600); e != nil {
		t.Fatal(e)
	}
	shortcutsFile := setupSteamShortcut(t, home, game, prefix)
	beforeShortcut, e := os.ReadFile(shortcutsFile)
	if e != nil {
		t.Fatal(e)
	}
	if e := install(game); e != nil {
		t.Fatal(e)
	}
	m, e := readManifest(stateDir())
	if e != nil || m.Version != 4 || m.Prefix != prefix || m.ShortcutAppID != "4292372109" {
		t.Fatalf("Wrong installation: %+v, %v", m, e)
	}
	if afterShortcut, e := os.ReadFile(shortcutsFile); e != nil || !bytes.Equal(afterShortcut, beforeShortcut) {
		t.Fatal("Install changed the D2R Steam shortcut")
	}
	if _, e := os.Stat(installedBinaryPath(game)); e != nil {
		t.Fatal("Install did not save the GUI launcher")
	}
	if _, e := os.Stat(filepath.Join(stateDir(), "license.so")); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(stateDir(), "game")); !os.IsNotExist(e) {
		t.Fatal("Game was copied")
	}
	// Refresh keeps the selected prefix even if another Steam prefix appears.
	other := filepath.Join(home, ".steam", "steam", "steamapps", "compatdata", "99999")
	if e := os.MkdirAll(filepath.Dir(cookiePath(other)), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(cookiePath(other), original, 0600); e != nil {
		t.Fatal(e)
	}
	if e := install(game); e != nil {
		t.Fatal(e)
	}
	m, e = readManifest(stateDir())
	if e != nil || m.Prefix != prefix {
		t.Fatal("Refresh changed the Proton prefix")
	}
	retargeted := bytes.Replace(beforeShortcut,
		[]byte(strconv.Quote(filepath.Join(game, "D2R.exe"))),
		[]byte(strconv.Quote(installedBinaryPath(game))), 1)
	if bytes.Equal(retargeted, beforeShortcut) {
		t.Fatal("Fixture D2R shortcut was not found")
	}
	if e := os.WriteFile(shortcutsFile, retargeted, 0600); e != nil {
		t.Fatal(e)
	}
	if e := uninstall(); e != nil {
		t.Fatal(e)
	}
	got, e := os.ReadFile(cookie)
	if e != nil || !bytes.Equal(got, original) {
		t.Fatal("Install or uninstall changed the cookie")
	}
	got, e = os.ReadFile(filepath.Join(game, "D2R.exe"))
	if e != nil || !bytes.Equal(got, []byte("D2R.exe")) {
		t.Fatal("Original game changed")
	}
	if _, e := readManifest(stateDir()); !os.IsNotExist(e) {
		t.Fatal("Offline installation record remained")
	}
	if _, e := os.Stat(installedBinaryPath(game)); e != nil {
		t.Fatal("Uninstall removed the GUI launcher")
	}
	if _, e := readSelection(stateDir()); e != nil {
		t.Fatal("Uninstall removed the saved paths")
	}
	if afterShortcut, e := os.ReadFile(shortcutsFile); e != nil || !bytes.Equal(afterShortcut, retargeted) {
		t.Fatal("Uninstall changed the D2R Steam shortcut")
	}
	if e := install(game); e != nil {
		t.Fatalf("Reinstall after uninstall failed: %v", e)
	}
	if m, e := readManifest(stateDir()); e != nil || m.Prefix != prefix || m.ShortcutAppID != "4292372109" || m.LaunchOptions == "" {
		t.Fatalf("Reinstall lost saved Steam settings: %+v, %v", m, e)
	}
	if e := uninstall(); e != nil {
		t.Fatal(e)
	}
}

func TestInstallWithoutCookie(t *testing.T) {
	allowFixtureBuild(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	game := filepath.Join(home, "game")
	if e := os.MkdirAll(game, 0700); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"D2R.exe", "D2R_loader.dll"} {
		if e := os.WriteFile(filepath.Join(game, name), []byte(name), 0600); e != nil {
			t.Fatal(e)
		}
	}
	prefix := filepath.Join(home, ".steam", "steam", "steamapps", "compatdata", "12345")
	if e := os.MkdirAll(filepath.Join(prefix, "pfx", "drive_c", "ProgramData", "Battle.net", "Agent"), 0700); e != nil {
		t.Fatal(e)
	}
	setupSteamShortcut(t, home, game, prefix)
	if e := install(game); e != nil {
		t.Fatal(e)
	}
	m, e := readManifest(stateDir())
	if e != nil || m.Prefix != prefix {
		t.Fatalf("Wrong prefix without a cookie: %+v, %v", m, e)
	}
	if e := uninstall(); e != nil {
		t.Fatal(e)
	}
}

func TestInstallRejectsUnsupportedGameBuild(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	game := filepath.Join(home, "game")
	if err := os.MkdirAll(game, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"D2R.exe", "D2R_loader.dll"} {
		if err := os.WriteFile(filepath.Join(game, name), []byte("unsupported fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := install(game); err == nil || !strings.Contains(err.Error(), "Windows executable") {
		t.Fatalf("Expected an invalid-PE error during Install, got %v", err)
	}
	if _, err := os.Stat(stateDir()); !os.IsNotExist(err) {
		t.Fatal("Unsupported build created installation state")
	}
}

func TestProtonMatchesPrefixVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	common := filepath.Join(home, ".steam", "steam", "steamapps", "common")
	for _, candidate := range []struct{ name, version string }{
		{"Proton 10.0", "1785139158 proton-10.0-4b"},
		{"Proton - Experimental", "1793301796 experimental-11.0-20261027b-x86_64"},
	} {
		folder := filepath.Join(common, candidate.name)
		if e := os.MkdirAll(folder, 0700); e != nil {
			t.Fatal(e)
		}
		for name, value := range map[string]string{"proton": "", "version": candidate.version} {
			if e := os.WriteFile(filepath.Join(folder, name), []byte(value), 0700); e != nil {
				t.Fatal(e)
			}
		}
	}
	runtime := filepath.Join(common, "SteamLinuxRuntime_sniper", "run")
	if e := os.MkdirAll(filepath.Dir(runtime), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(runtime, nil, 0700); e != nil {
		t.Fatal(e)
	}
	prefix := filepath.Join(home, "prefix")
	if e := os.MkdirAll(prefix, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(prefix, "version"), []byte("10.1000-105"), 0600); e != nil {
		t.Fatal(e)
	}
	_, proton, _, e := protonPaths(prefix)
	if e != nil || filepath.Base(filepath.Dir(proton)) != "Proton 10.0" {
		t.Fatalf("Selected %q: %v", proton, e)
	}
}

func TestProtonUsesSteamConfiguredTool(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	steam := filepath.Join(home, ".steam", "steam")
	common := filepath.Join(steam, "steamapps", "common")
	for _, name := range []string{"Proton 10.0", "Proton 11.0"} {
		folder := filepath.Join(common, name)
		if err := os.MkdirAll(folder, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, "proton"), nil, 0700); err != nil {
			t.Fatal(err)
		}
	}
	runtime := filepath.Join(common, "SteamLinuxRuntime_sniper", "run")
	if err := os.MkdirAll(filepath.Dir(runtime), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtime, nil, 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(steam, "config", "config.vdf")
	if err := os.MkdirAll(filepath.Dir(config), 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`"InstallConfigStore" { "Software" { "Valve" { "Steam" { "CompatToolMapping" { "12345" { "name" "proton_10" } } } } } }`)
	if err := os.WriteFile(config, data, 0600); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(steam, "steamapps", "compatdata", "12345")
	if err := os.MkdirAll(prefix, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefix, "version"), []byte("11.0-100"), 0600); err != nil {
		t.Fatal(err)
	}
	_, proton, _, err := protonPaths(prefix)
	if err != nil || filepath.Base(filepath.Dir(proton)) != "Proton 10.0" {
		t.Fatalf("Selected %q: %v", proton, err)
	}
}
