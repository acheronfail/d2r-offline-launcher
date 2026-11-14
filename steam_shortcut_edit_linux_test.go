//go:build linux

package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func fixtureField(kind byte, key, value string) []byte {
	return append(append(append([]byte{kind}, key...), 0), append([]byte(value), 0)...)
}

func fixtureShortcut(index string, id uint32, name, exe, dir, options string) []byte {
	entry := append([]byte{0}, []byte(index+"\x00")...)
	entry = append(entry, []byte{2, 'a', 'p', 'p', 'i', 'd', 0}...)
	var number [4]byte
	binary.LittleEndian.PutUint32(number[:], id)
	entry = append(entry, number[:]...)
	for _, field := range []struct{ key, value string }{
		{"AppName", name}, {"Exe", exe}, {"StartDir", dir}, {"LaunchOptions", options},
	} {
		entry = append(entry, fixtureField(1, field.key, field.value)...)
	}
	return append(entry, 8)
}

func setupSteamShortcut(t *testing.T, home, game, prefix string) string {
	t.Helper()
	file := filepath.Join(home, ".steam", "steam", "userdata", "123", "config", "shortcuts.vdf")
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	data := append([]byte{0}, []byte("shortcuts\x00")...)
	data = append(data, fixtureShortcut("0", 2612526253, "Battle.net", strconv.Quote(filepath.Join(home, "Battle.net.exe")), home, "")...)
	data = append(data, fixtureShortcut("1", 4292372109, "Diablo II Resurrected", strconv.Quote(filepath.Join(game, "D2R.exe")), strconv.Quote(game), "STEAM_COMPAT_DATA_PATH="+strconv.Quote(prefix)+" %command% -direct -txt")...)
	data = append(data, 8, 8)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("D2R_CAMPING_SHORTCUTS_FILE", file)
	return file
}

func TestShortcutEditRestoresOnlyD2RFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	game := filepath.Join(home, "Games", "Diablo II Resurrected")
	if err := os.MkdirAll(game, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(game, "D2R.exe"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(home, ".local", "share", "d2r-camping", "D2R-Camping-Installer")
	if err := os.MkdirAll(filepath.Dir(installed), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, []byte("app"), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(home, ".steam", "steam", "userdata", "123", "config", "shortcuts.vdf")
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	data := append([]byte{0}, []byte("shortcuts\x00")...)
	bnet := fixtureShortcut("0", 1234, "Battle.net", strconv.Quote(filepath.Join(home, "Battle.net.exe")), home, "")
	data = append(data, bnet...)
	options := `STEAM_COMPAT_DATA_PATH="/some/prefix" %command% -direct -txt`
	data = append(data, fixtureShortcut("1", 4292372109, "Diablo II Resurrected", strconv.Quote(filepath.Join(game, "D2R.exe")), strconv.Quote(game), options)...)
	data = append(data, 8, 8)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	record, err := findShortcut(game, "")
	if err != nil {
		t.Fatal(err)
	}
	if record.AppID != "4292372109" || record.LaunchOptions != options {
		t.Fatalf("Unexpected original shortcut: %+v", record)
	}
	if err := changeShortcut(record, filepath.Join(game, "D2R.exe"), strconv.Quote(installed), strconv.Quote(filepath.Dir(installed)), ""); err != nil {
		t.Fatal(err)
	}
	changed, err := os.ReadFile(file)
	if err != nil || bytes.Equal(changed, data) || !bytes.Contains(changed, bnet) {
		t.Fatal("D2R shortcut was not changed or Battle.net shortcut was changed")
	}
	current, err := findShortcut(game, installed)
	if err != nil || current.AppID != record.AppID || !sameExecutable(current.Exe, installed) {
		t.Fatalf("Installed shortcut was not found: %+v, %v", current, err)
	}
	if err := changeShortcut(record, installed, record.Exe, record.StartDir, record.LaunchOptions); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(restored, data) {
		t.Fatal("Restored shortcuts.vdf differs from its original bytes")
	}
}

func TestInspectLiveShortcutReadOnly(t *testing.T) {
	game := os.Getenv("D2R_CAMPING_INSPECT_GAME")
	if game == "" {
		t.Skip("set D2R_CAMPING_INSPECT_GAME for a read-only live shortcut check")
	}
	installed := os.Getenv("D2R_CAMPING_INSPECT_INSTALLED")
	record, err := findShortcut(game, installed)
	if err != nil {
		t.Fatal(err)
	}
	if record.AppID == "0" || (!sameExecutable(record.Exe, filepath.Join(game, "D2R.exe")) &&
		(installed == "" || !sameExecutable(record.Exe, installed))) {
		t.Fatalf("Wrong live shortcut: %+v", record)
	}
	t.Logf("Found live D2R shortcut app ID %s and saved Proton launch options", record.AppID)
}
