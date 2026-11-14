//go:build linux

package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func TestShortcutAppIDUsesGameShortcutNotSharedPrefix(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("D2R_CAMPING_PREFIX", "")
	game := filepath.Join(home, "Games", "Diablo II Resurrected")
	if err := os.MkdirAll(game, 0700); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(game, "D2R.exe")
	if err := os.WriteFile(exe, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(game, "D2R_loader.dll"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(home, ".steam", "steam", "steamapps", "compatdata", "2612526253")
	if err := os.MkdirAll(filepath.Join(prefix, "pfx"), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(home, ".steam", "steam", "userdata", "123", "config", "shortcuts.vdf")
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	data := append([]byte{0}, []byte("shortcuts\x00")...)
	appendShortcut := func(index byte, id uint32, path, options string) {
		data = append(data, 0, index, 0)
		data = append(data, []byte{2, 'a', 'p', 'p', 'i', 'd', 0}...)
		var number [4]byte
		binary.LittleEndian.PutUint32(number[:], id)
		data = append(data, number[:]...)
		data = append(data, []byte{1, 'E', 'x', 'e', 0}...)
		data = append(data, []byte(strconv.Quote(path))...)
		data = append(data, 0, 8)
		if options != "" {
			data = data[:len(data)-1]
			data = append(data, []byte{1, 'L', 'a', 'u', 'n', 'c', 'h', 'O', 'p', 't', 'i', 'o', 'n', 's', 0}...)
			data = append(data, []byte(options)...)
			data = append(data, 0, 8)
		}
	}
	appendShortcut('0', 2612526253, filepath.Join(home, "Games", "Battle.net.exe"), "")
	appendShortcut('1', 4292372109, exe,
		"STEAM_COMPAT_DATA_PATH="+strconv.Quote(prefix)+` %command% -direct -txt -mod "My Mod"`)
	data = append(data, 8)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	if got := shortcutAppID(game); got != "4292372109" {
		t.Fatalf("D2R shortcut ID = %q, want 4292372109", got)
	}
	if got := shortcutPrefix(game); got != prefix {
		t.Fatalf("D2R shortcut prefix = %q, want %q", got, prefix)
	}
	if got := choosePrefix(); got != prefix {
		t.Fatalf("Detected prefix = %q, want D2R shortcut prefix %q", got, prefix)
	}
	wantArgs := []string{"-direct", "-txt", "-mod", "My Mod"}
	if got := gameArgsFromOptions(shortcutForGame(game).LaunchOptions); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("D2R launch args = %q, want %q", got, wantArgs)
	}
}
