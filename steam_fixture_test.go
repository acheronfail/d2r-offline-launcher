package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func setupSteamShortcut(t *testing.T, home, game, prefix string) string {
	t.Helper()
	file := filepath.Join(home, ".steam", "steam", "userdata", "123", "config", "shortcuts.vdf")
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	field := func(key, value string) []byte {
		return append(append(append([]byte{1}, key...), 0), append([]byte(value), 0)...)
	}
	shortcut := func(index string, id uint32, name, exe, dir, options string) []byte {
		entry := append([]byte{0}, []byte(index+"\x00")...)
		entry = append(entry, []byte{2, 'a', 'p', 'p', 'i', 'd', 0}...)
		var number [4]byte
		binary.LittleEndian.PutUint32(number[:], id)
		entry = append(entry, number[:]...)
		for _, item := range [][2]string{{"AppName", name}, {"Exe", exe}, {"StartDir", dir}, {"LaunchOptions", options}} {
			entry = append(entry, field(item[0], item[1])...)
		}
		return append(entry, 8)
	}
	data := append([]byte{0}, []byte("shortcuts\x00")...)
	data = append(data, shortcut("0", 2612526253, "Battle.net", strconv.Quote(filepath.Join(home, "Battle.net.exe")), home, "")...)
	data = append(data, shortcut("1", 4292372109, "Diablo II Resurrected", strconv.Quote(filepath.Join(game, "D2R.exe")), strconv.Quote(game), "STEAM_COMPAT_DATA_PATH="+strconv.Quote(prefix)+" %command% -direct -txt")...)
	data = append(data, 8, 8)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("D2R_OFFLINE_SHORTCUTS_FILE", file)
	return file
}
