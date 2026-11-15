package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMetadataDetails(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, "game")
	prefix := filepath.Join(root, "prefix")
	if err := os.MkdirAll(game, 0700); err != nil {
		t.Fatal(err)
	}
	build := "Branch!STRING:0|Active!DEC:1|Version!STRING:0\nus|0|1.0\neu|1|3.3.93847\n"
	if err := os.WriteFile(filepath.Join(game, ".build.info"), []byte(build), 0600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(game, "D2R.exe")
	if err := os.WriteFile(exe, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	cookie := cookiePath(prefix)
	if err := os.MkdirAll(filepath.Dir(cookie), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cookie, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.November, 15, 12, 0, 0, 0, time.Local)
	modified := now.Add(-49 * time.Hour)
	for _, path := range []string{exe, cookie} {
		if err := os.Chtimes(path, modified, modified); err != nil {
			t.Fatal(err)
		}
	}
	m := metadataDetails(game, prefix, now)
	if m.gameVersion != "3.3.93847" || m.gameModified == "Unavailable" || m.cookieState != "File present" || m.cookieModified == "Unavailable" || m.cookieAge != "2 days" {
		t.Fatalf("unexpected metadata: %+v", m)
	}
	if err := os.Remove(cookie); err != nil {
		t.Fatal(err)
	}
	m = metadataDetails(game, prefix, now)
	if m.cookieState != "Not found" || m.cookieAge != "Unavailable" {
		t.Fatalf("unexpected missing-cookie metadata: %+v", m)
	}
}
