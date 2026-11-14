//go:build linux

package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Steam stores non-Steam shortcuts as binary KeyValues. Keep every field that
// does not belong to the D2R shortcut byte-for-byte when changing its target.
type kvEntry struct {
	name              string
	kind              byte
	start, value, end int
	children          []kvEntry
}

func kvCString(data []byte, pos *int) (string, error) {
	if *pos >= len(data) {
		return "", errors.New("truncated Steam shortcut")
	}
	end := bytes.IndexByte(data[*pos:], 0)
	if end < 0 {
		return "", errors.New("unterminated Steam shortcut string")
	}
	value := string(data[*pos : *pos+end])
	*pos += end + 1
	return value, nil
}

func kvEntries(data []byte, pos *int, depth int) ([]kvEntry, error) {
	if depth > 8 {
		return nil, errors.New("Steam shortcut nesting is too deep")
	}
	var entries []kvEntry
	for *pos < len(data) {
		start := *pos
		kind := data[*pos]
		*pos++
		if kind == 8 {
			return entries, nil
		}
		name, err := kvCString(data, pos)
		if err != nil {
			return nil, err
		}
		entry := kvEntry{name: name, kind: kind, start: start, value: *pos}
		switch kind {
		case 0:
			entry.children, err = kvEntries(data, pos, depth+1)
		case 1:
			_, err = kvCString(data, pos)
		case 2, 3, 4, 6:
			if *pos+4 > len(data) {
				err = errors.New("truncated Steam shortcut number")
			} else {
				*pos += 4
			}
		case 7:
			if *pos+8 > len(data) {
				err = errors.New("truncated Steam shortcut number")
			} else {
				*pos += 8
			}
		case 9, 10, 11:
			// KeyValues compact integer encodings.
			if kind == 9 {
				if *pos >= len(data) {
					err = errors.New("truncated Steam shortcut byte")
				} else {
					*pos++
				}
			}
		default:
			err = fmt.Errorf("unsupported Steam shortcut field type %d", kind)
		}
		if err != nil {
			return nil, err
		}
		entry.end = *pos
		entries = append(entries, entry)
	}
	return nil, fmt.Errorf("Steam shortcut object is missing its end marker at depth %d, offset %d", depth, *pos)
}

func parseShortcuts(data []byte) ([]kvEntry, error) {
	pos := 0
	entries, err := kvEntries(data, &pos, 0)
	if err != nil {
		return nil, err
	}
	if pos != len(data) || len(entries) != 1 || entries[0].kind != 0 || entries[0].name != "shortcuts" {
		return nil, errors.New("Steam shortcuts.vdf has an unexpected structure")
	}
	return entries[0].children, nil
}

func kvChild(parent kvEntry, name string, kind byte) (kvEntry, bool) {
	for _, child := range parent.children {
		if child.name == name && child.kind == kind {
			return child, true
		}
	}
	return kvEntry{}, false
}

func kvText(data []byte, parent kvEntry, name string) string {
	field, ok := kvChild(parent, name, 1)
	if !ok {
		return ""
	}
	return string(data[field.value : field.end-1])
}

func kvAppID(data []byte, parent kvEntry) uint32 {
	field, ok := kvChild(parent, "appid", 2)
	if !ok {
		return 0
	}
	return binary.LittleEndian.Uint32(data[field.value:field.end])
}

type shortcutRecord struct {
	File, AppID, Name, Exe, StartDir, LaunchOptions string
}

func sameExecutable(value, target string) bool {
	path := filepath.Clean(strings.Trim(value, `"'`))
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}
	return path == target
}

func findShortcut(game, installedExe string) (shortcutRecord, error) {
	if override := os.Getenv("D2R_CAMPING_SHORTCUTS_FILE"); override != "" {
		return shortcutInFiles(game, installedExe, []string{override})
	}
	var files []string
	for _, root := range steamRoots() {
		users, _ := os.ReadDir(filepath.Join(root, "userdata"))
		for _, user := range users {
			files = append(files, filepath.Join(root, "userdata", user.Name(), "config", "shortcuts.vdf"))
		}
	}
	return shortcutInFiles(game, installedExe, files)
}

func shortcutInFiles(game, installedExe string, files []string) (shortcutRecord, error) {
	var matches []shortcutRecord
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil || len(data) > 16<<20 {
			continue
		}
		records, err := parseShortcuts(data)
		if err != nil {
			return shortcutRecord{}, fmt.Errorf("Read %s: %w", file, err)
		}
		for _, record := range records {
			exe := kvText(data, record, "Exe")
			if !sameExecutable(exe, filepath.Join(game, "D2R.exe")) &&
				(installedExe == "" || !sameExecutable(exe, installedExe)) {
				continue
			}
			matches = append(matches, shortcutRecord{File: file,
				AppID: strconv.FormatUint(uint64(kvAppID(data, record)), 10),
				Name:  kvText(data, record, "AppName"), Exe: exe,
				StartDir:      kvText(data, record, "StartDir"),
				LaunchOptions: kvText(data, record, "LaunchOptions")})
		}
	}
	// ~/.steam/steam and ~/.local/share/Steam commonly refer to the same file.
	unique := make(map[string]shortcutRecord)
	for _, match := range matches {
		path, err := filepath.EvalSymlinks(match.File)
		if err != nil {
			path = match.File
		}
		unique[path+"/"+match.AppID] = match
	}
	if len(unique) != 1 {
		return shortcutRecord{}, fmt.Errorf("Expected one D2R Steam shortcut, found %d", len(unique))
	}
	for _, record := range unique {
		if record.AppID == "0" || record.Name == "" {
			return shortcutRecord{}, errors.New("D2R Steam shortcut is missing its app ID or name")
		}
		return record, nil
	}
	panic("unreachable")
}

type kvReplacement struct {
	start, end int
	value      string
}

func changeShortcut(record shortcutRecord, expectedExe, exe, dir, options string) error {
	data, err := os.ReadFile(record.File)
	if err != nil {
		return err
	}
	entries, err := parseShortcuts(data)
	if err != nil {
		return err
	}
	var found *kvEntry
	for i := range entries {
		if strconv.FormatUint(uint64(kvAppID(data, entries[i])), 10) != record.AppID {
			continue
		}
		if kvText(data, entries[i], "AppName") != record.Name ||
			!sameExecutable(kvText(data, entries[i], "Exe"), expectedExe) {
			return errors.New("D2R Steam shortcut changed; refusing to overwrite it")
		}
		found = &entries[i]
		break
	}
	if found == nil {
		return errors.New("D2R Steam shortcut was not found for editing")
	}
	var replacements []kvReplacement
	for name, value := range map[string]string{"Exe": exe, "StartDir": dir, "LaunchOptions": options} {
		field, ok := kvChild(*found, name, 1)
		if !ok {
			return fmt.Errorf("D2R Steam shortcut is missing %s", name)
		}
		replacements = append(replacements, kvReplacement{field.value, field.end - 1, value})
	}
	// Replace from the end so offsets in the parsed file stay valid.
	for i := 0; i < len(replacements); i++ {
		for j := i + 1; j < len(replacements); j++ {
			if replacements[j].start > replacements[i].start {
				replacements[i], replacements[j] = replacements[j], replacements[i]
			}
		}
	}
	for _, replacement := range replacements {
		data = bytes.Join([][]byte{data[:replacement.start], []byte(replacement.value), data[replacement.end:]}, nil)
	}
	info, err := os.Stat(record.File)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(record.File), ".d2r-shortcuts-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err = tmp.Chmod(info.Mode().Perm()); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), record.File)
}

func installedBinaryPath(state string) string {
	return filepath.Join(state, "D2R-Camping-Installer")
}

func copyInstaller(state string) error {
	source, err := os.Executable()
	if err != nil {
		return err
	}
	target := installedBinaryPath(state)
	if src, err := os.Stat(source); err == nil {
		if dst, err := os.Stat(target); err == nil && os.SameFile(src, dst) {
			return nil
		}
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(state, ".installer-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	if err = out.Chmod(0700); err == nil {
		_, err = io.Copy(out, in)
	}
	if err == nil {
		err = out.Sync()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(out.Name(), target)
}

func shortcutFromManifest(m manifest) shortcutRecord {
	return shortcutRecord{File: m.ShortcutFile, AppID: m.ShortcutAppID,
		Name: m.ShortcutName, Exe: m.OriginalExe,
		StartDir: m.OriginalStartDir, LaunchOptions: m.OriginalLaunchOptions}
}

func prepareShortcutInstall(state string, m *manifest) error {
	if m.ShortcutFile == "" {
		record, err := findShortcut(m.Game, "")
		if err != nil {
			return err
		}
		m.ShortcutFile, m.ShortcutAppID, m.ShortcutName = record.File, record.AppID, record.Name
		m.OriginalExe, m.OriginalStartDir = record.Exe, record.StartDir
		m.OriginalLaunchOptions = record.LaunchOptions
		m.CompatFile = compatFileForShortcut(record.File)
		if data, err := os.ReadFile(m.CompatFile); err == nil {
			m.OriginalCompatBlock, err = compatBlock(data, m.ShortcutAppID)
			if err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return copyInstaller(state)
}

func activateShortcut(state string, m manifest) error {
	current, err := shortcutInFiles(m.Game, installedBinaryPath(state), []string{m.ShortcutFile})
	if err != nil {
		return err
	}
	if current.AppID != m.ShortcutAppID || current.Name != m.ShortcutName {
		return errors.New("Steam changed the D2R shortcut identity; leave it untouched and inspect the new app ID")
	}
	if !sameExecutable(current.Exe, installedBinaryPath(state)) {
		if !sameExecutable(current.Exe, filepath.Join(m.Game, "D2R.exe")) {
			return errors.New("D2R shortcut target changed; refusing to overwrite it")
		}
		if err := changeShortcut(shortcutFromManifest(m), filepath.Join(m.Game, "D2R.exe"),
			strconv.Quote(installedBinaryPath(state)), strconv.Quote(state), ""); err != nil {
			return err
		}
	}
	if m.CompatFile != "" {
		return editCompatMapping(m.CompatFile, m.ShortcutAppID, m.OriginalCompatBlock, false)
	}
	return nil
}

func restoreShortcut(state string, m manifest) error {
	current, err := shortcutInFiles(m.Game, installedBinaryPath(state), []string{m.ShortcutFile})
	if err != nil {
		return err
	}
	if current.AppID != m.ShortcutAppID || current.Name != m.ShortcutName {
		return errors.New("Steam changed the D2R shortcut identity; refusing automatic restoration")
	}
	if !sameExecutable(current.Exe, filepath.Join(m.Game, "D2R.exe")) {
		if err := changeShortcut(shortcutFromManifest(m), installedBinaryPath(state),
			m.OriginalExe, m.OriginalStartDir, m.OriginalLaunchOptions); err != nil {
			return err
		}
	}
	if m.CompatFile != "" {
		return editCompatMapping(m.CompatFile, m.ShortcutAppID, m.OriginalCompatBlock, true)
	}
	return nil
}
