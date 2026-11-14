//go:build linux

package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type steamShortcut struct {
	AppID         string
	LaunchOptions string
}

func shortcutField(segment []byte, name string) string {
	marker := append([]byte{1}, []byte(name+"\x00")...)
	at := bytes.Index(segment, marker)
	if at < 0 {
		return ""
	}
	value := segment[at+len(marker):]
	if end := bytes.IndexByte(value, 0); end >= 0 {
		return string(value[:end])
	}
	return ""
}

// A D2R shortcut can use another shortcut's Proton prefix. Read the app ID
// attached to D2R.exe for its offline launch.
func shortcutForGame(game string) steamShortcut {
	target := filepath.Join(game, "D2R.exe")
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}
	const maxShortcutsFile = 16 << 20
	appIDField := []byte{2, 'a', 'p', 'p', 'i', 'd', 0}
	for _, root := range steamRoots() {
		users, _ := os.ReadDir(filepath.Join(root, "userdata"))
		for _, user := range users {
			if !user.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(root, "userdata", user.Name(), "config", "shortcuts.vdf"))
			if err != nil || len(data) > maxShortcutsFile {
				continue
			}
			for offset := 0; offset < len(data); {
				found := bytes.Index(data[offset:], appIDField)
				if found < 0 {
					break
				}
				start := offset + found
				value := start + len(appIDField)
				if value+4 > len(data) {
					break
				}
				id := binary.LittleEndian.Uint32(data[value : value+4])
				next := bytes.Index(data[value+4:], appIDField)
				end := len(data)
				if next >= 0 {
					end = value + 4 + next
				}
				segment := data[value+4 : end]
				if exe := shortcutField(segment, "Exe"); exe != "" {
					path := filepath.Clean(strings.Trim(exe, `"'`))
					if resolved, err := filepath.EvalSymlinks(path); err == nil {
						path = resolved
					}
					if id != 0 && path == target {
						return steamShortcut{AppID: strconv.FormatUint(uint64(id), 10),
							LaunchOptions: shortcutField(segment, "LaunchOptions")}
					}
				}
				offset = end
			}
		}
	}
	return steamShortcut{}
}

func shortcutAppID(game string) string { return shortcutForGame(game).AppID }

// Split the argument portion of Steam Launch Options.
func splitLaunchWords(input string) ([]string, bool) {
	var words []string
	var current strings.Builder
	var quote byte
	escaped, started := false, false
	flush := func() {
		if started {
			words = append(words, current.String())
			current.Reset()
			started = false
		}
	}
	for i := 0; i < len(input); i++ {
		ch := input[i]
		switch {
		case escaped:
			current.WriteByte(ch)
			escaped = false
		case ch == '\\' && quote != '\'':
			escaped = true
			started = true
		case quote != 0:
			if ch == quote {
				quote = 0
			} else {
				current.WriteByte(ch)
			}
		case ch == '"' || ch == '\'':
			quote = ch
			started = true
		case ch == ' ' || ch == '\t' || ch == '\n':
			flush()
		default:
			current.WriteByte(ch)
			started = true
		}
	}
	if quote != 0 || escaped {
		return nil, false
	}
	flush()
	return words, true
}

func shortcutPrefix(game string) string {
	words, ok := splitLaunchWords(shortcutForGame(game).LaunchOptions)
	if !ok {
		return ""
	}
	for _, word := range words {
		if word == "%command%" {
			break
		}
		if path, found := strings.CutPrefix(word, "STEAM_COMPAT_DATA_PATH="); found {
			if info, err := os.Stat(filepath.Join(path, "pfx")); err == nil && info.IsDir() {
				return path
			}
		}
	}
	return ""
}

func gameArgsFromOptions(options string) []string {
	words, ok := splitLaunchWords(options)
	if !ok {
		return []string{"-direct"}
	}
	for i, word := range words {
		if word == "%command%" {
			args := append([]string(nil), words[i+1:]...)
			for _, arg := range args {
				if arg == "-direct" {
					return args
				}
			}
			return append([]string{"-direct"}, args...)
		}
	}
	return []string{"-direct"}
}
