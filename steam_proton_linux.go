//go:build linux

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var prefixMajorPattern = regexp.MustCompile(`^\s*([0-9]+)\.`)
var protonMajorPattern = regexp.MustCompile(`(?:proton|experimental)-([0-9]+)\.`)

func vdfObject(data []byte, name string) []byte {
	marker := []byte(`"` + name + `"`)
	for search := 0; search < len(data); {
		found := bytes.Index(data[search:], marker)
		if found < 0 {
			return nil
		}
		open := search + found + len(marker)
		for open < len(data) && bytes.IndexByte([]byte(" \t\r\n"), data[open]) >= 0 {
			open++
		}
		if open == len(data) || data[open] != '{' {
			search = open
			continue
		}
		depth, quoted, escaped := 0, false, false
		for i := open; i < len(data); i++ {
			ch := data[i]
			if quoted {
				if escaped {
					escaped = false
				} else if ch == '\\' {
					escaped = true
				} else if ch == '"' {
					quoted = false
				}
				continue
			}
			switch ch {
			case '"':
				quoted = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return data[open+1 : i]
				}
			}
		}
		return nil
	}
	return nil
}

func vdfString(data []byte, name string) string {
	marker := []byte(`"` + name + `"`)
	start := bytes.Index(data, marker)
	if start < 0 {
		return ""
	}
	value := bytes.TrimSpace(data[start+len(marker):])
	if len(value) == 0 || value[0] != '"' {
		return ""
	}
	end := bytes.IndexByte(value[1:], '"')
	if end < 0 {
		return ""
	}
	return string(value[1 : end+1])
}

func configuredProton(prefix string) string {
	appID := filepath.Base(filepath.Clean(prefix))
	if _, err := strconv.ParseUint(appID, 10, 32); err != nil {
		return ""
	}
	for _, root := range steamRoots() {
		data, err := os.ReadFile(filepath.Join(root, "config", "config.vdf"))
		if err != nil || len(data) > 16<<20 {
			continue
		}
		tool := vdfString(vdfObject(vdfObject(data, "CompatToolMapping"), appID), "name")
		if tool == "proton_experimental" {
			return "Proton - Experimental"
		}
		if major, found := strings.CutPrefix(tool, "proton_"); found {
			if _, err := strconv.Atoi(major); err == nil {
				return "Proton " + major + ".0"
			}
		}
		if tool != "" {
			return tool
		}
	}
	return ""
}
