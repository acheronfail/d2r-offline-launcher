//go:build linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type textVDFNode struct {
	key         string
	start, end  int
	open, close int
	children    []textVDFNode
}

func skipVDFSpace(data []byte, pos *int) {
	for *pos < len(data) {
		if bytes.ContainsAny(data[*pos:*pos+1], " \t\r\n") {
			*pos++
			continue
		}
		if *pos+1 < len(data) && data[*pos] == '/' && data[*pos+1] == '/' {
			for *pos < len(data) && data[*pos] != '\n' {
				*pos++
			}
			continue
		}
		break
	}
}

func quotedVDF(data []byte, pos *int) (string, error) {
	if *pos >= len(data) || data[*pos] != '"' {
		return "", errors.New("expected quoted Steam config key or value")
	}
	*pos++
	start := *pos
	for *pos < len(data) {
		if data[*pos] == '\\' {
			*pos += 2
			continue
		}
		if data[*pos] == '"' {
			value := string(data[start:*pos])
			*pos++
			return value, nil
		}
		*pos++
	}
	return "", errors.New("unterminated Steam config string")
}

func parseTextVDF(data []byte, pos *int, depth int, nested bool) ([]textVDFNode, error) {
	if depth > 32 {
		return nil, errors.New("Steam config nesting is too deep")
	}
	var nodes []textVDFNode
	for {
		skipVDFSpace(data, pos)
		if *pos == len(data) {
			if nested {
				return nil, errors.New("Steam config object is truncated")
			}
			return nodes, nil
		}
		if data[*pos] == '}' {
			if !nested {
				return nil, errors.New("unexpected Steam config closing brace")
			}
			*pos++
			return nodes, nil
		}
		node := textVDFNode{start: *pos}
		key, err := quotedVDF(data, pos)
		if err != nil {
			return nil, err
		}
		node.key = key
		skipVDFSpace(data, pos)
		if *pos < len(data) && data[*pos] == '{' {
			node.open = *pos
			*pos++
			node.children, err = parseTextVDF(data, pos, depth+1, true)
			node.close = *pos - 1
		} else {
			_, err = quotedVDF(data, pos)
		}
		if err != nil {
			return nil, err
		}
		node.end = *pos
		nodes = append(nodes, node)
	}
}

func childVDF(nodes []textVDFNode, name string) (textVDFNode, bool) {
	for _, node := range nodes {
		if node.key == name {
			return node, true
		}
	}
	return textVDFNode{}, false
}

func compatMapping(data []byte, appID string) (textVDFNode, textVDFNode, error) {
	pos := 0
	nodes, err := parseTextVDF(data, &pos, 0, false)
	if err != nil {
		return textVDFNode{}, textVDFNode{}, err
	}
	for _, key := range []string{"InstallConfigStore", "Software", "Valve", "Steam", "CompatToolMapping"} {
		root, ok := childVDF(nodes, key)
		if !ok || root.open == 0 {
			return textVDFNode{}, textVDFNode{}, fmt.Errorf("Steam config lacks %s", key)
		}
		nodes = root.children
		if key == "CompatToolMapping" {
			entry, _ := childVDF(nodes, appID)
			return root, entry, nil
		}
	}
	panic("unreachable")
}

func compatBlock(data []byte, appID string) (string, error) {
	_, entry, err := compatMapping(data, appID)
	if err != nil || entry.key == "" {
		return "", err
	}
	start := bytes.LastIndexByte(data[:entry.start], '\n') + 1
	end := entry.end
	if end < len(data) && data[end] == '\r' {
		end++
	}
	if end < len(data) && data[end] == '\n' {
		end++
	}
	return string(data[start:end]), nil
}

func editCompatMapping(file, appID, originalBlock string, restoring bool) error {
	data, err := os.ReadFile(file)
	if os.IsNotExist(err) && originalBlock == "" {
		return nil
	}
	if err != nil {
		return err
	}
	parent, entry, err := compatMapping(data, appID)
	if err != nil {
		return err
	}
	if restoring && originalBlock != "" {
		if entry.key != "" {
			return errors.New("D2R Steam compatibility setting changed; refusing to overwrite it")
		}
		at := bytes.LastIndexByte(data[:parent.close], '\n') + 1
		data = bytes.Join([][]byte{data[:at], []byte(originalBlock), data[at:]}, nil)
	} else if entry.key != "" {
		start := bytes.LastIndexByte(data[:entry.start], '\n') + 1
		end := entry.end
		if end < len(data) && data[end] == '\r' {
			end++
		}
		if end < len(data) && data[end] == '\n' {
			end++
		}
		data = bytes.Join([][]byte{data[:start], data[end:]}, nil)
	} else {
		return nil
	}
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".d2r-compat-*")
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
	return os.Rename(tmp.Name(), file)
}

func compatFileForShortcut(shortcutsFile string) string {
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(shortcutsFile))))
	return filepath.Join(root, "config", "config.vdf")
}
