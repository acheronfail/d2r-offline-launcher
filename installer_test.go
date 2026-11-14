package main

import (
	"bytes"
	"os"
	"path/filepath"
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
	if current, e := findShortcut(game, installedBinaryPath(stateDir())); e != nil || !sameExecutable(current.Exe, installedBinaryPath(stateDir())) {
		t.Fatalf("Install did not redirect D2R shortcut: %+v, %v", current, e)
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
	if _, e := os.Stat(stateDir()); !os.IsNotExist(e) {
		t.Fatal("Installation directory remained")
	}
	if afterShortcut, e := os.ReadFile(shortcutsFile); e != nil || !bytes.Equal(afterShortcut, beforeShortcut) {
		t.Fatal("Uninstall did not restore the original D2R shortcut")
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
		{"Proton 10.0", "10.0-4"},
		{"Proton - Experimental", "experimental-11.0-20261105c"},
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
	if e := os.WriteFile(filepath.Join(prefix, "version"), []byte("10.0-100"), 0600); e != nil {
		t.Fatal(e)
	}
	_, proton, _, e := protonPaths(prefix)
	if e != nil || filepath.Base(filepath.Dir(proton)) != "Proton 10.0" {
		t.Fatalf("Selected %q: %v", proton, e)
	}
}
