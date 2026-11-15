package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type launcherMetadata struct {
	gameVersion    string
	gameModified   string
	cookieState    string
	cookieModified string
	cookieAge      string
}

func buildVersion(game string) string {
	f, err := os.Open(filepath.Join(game, ".build.info"))
	if err != nil {
		return "Unavailable"
	}
	defer f.Close()
	scanner := bufio.NewScanner(io.LimitReader(f, 64*1024))
	if !scanner.Scan() {
		return "Unavailable"
	}
	headers := strings.Split(strings.TrimPrefix(scanner.Text(), "\ufeff"), "|")
	active, version := -1, -1
	for i, header := range headers {
		switch strings.SplitN(header, "!", 2)[0] {
		case "Active":
			active = i
		case "Version":
			version = i
		}
	}
	if active < 0 || version < 0 {
		return "Unavailable"
	}
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "|")
		if active < len(fields) && version < len(fields) && fields[active] == "1" {
			if value := strings.TrimSpace(fields[version]); value != "" {
				return value
			}
		}
	}
	return "Unavailable"
}

func metadataDetails(game, prefix string, now time.Time) launcherMetadata {
	m := launcherMetadata{
		gameVersion: "Unavailable", gameModified: "Unavailable",
		cookieState: "Not found", cookieModified: "Unavailable", cookieAge: "Unavailable",
	}
	if game != "" {
		m.gameVersion = buildVersion(game)
		if info, err := os.Stat(filepath.Join(game, "D2R.exe")); err == nil && !info.IsDir() {
			m.gameModified = info.ModTime().In(now.Location()).Format("2 Jan 2006, 3:04 pm")
		}
	}
	if prefix == "" {
		return m
	}
	info, err := os.Stat(cookiePath(prefix))
	if err != nil || info.IsDir() {
		return m
	}
	modified := info.ModTime().In(now.Location())
	m.cookieModified = modified.Format("2 Jan 2006, 3:04 pm")
	if info.Size() == 0 {
		m.cookieState = "Empty"
		return m
	}
	if _, _, err := offlineClockOffset(prefix, now); err != nil {
		m.cookieState = "Timestamp out of range"
		return m
	}
	m.cookieState = "File present"
	age := now.Sub(modified)
	if age < time.Hour {
		m.cookieAge = "Less than an hour"
	} else if age < 24*time.Hour {
		m.cookieAge = fmt.Sprintf("%d hours", int(age.Hours()))
	} else {
		m.cookieAge = fmt.Sprintf("%d days", int(age.Hours()/24))
	}
	return m
}
