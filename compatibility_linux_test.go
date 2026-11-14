//go:build linux

package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// PE preflight accepts a modified executable; the live shim checks its
// structure at launch.
func TestPEPreflightAcceptsUnrelatedFileChanges(t *testing.T) {
	game := os.Getenv("D2R_OFFLINE_INSPECT_GAME")
	if game == "" {
		t.Skip("set D2R_OFFLINE_INSPECT_GAME to test a real executable")
	}
	if err := verifyGameBuild(game); err != nil {
		t.Fatal(err)
	}
	copyDir := t.TempDir()
	in, err := os.Open(filepath.Join(game, "D2R.exe"))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(filepath.Join(copyDir, "D2R.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(copyDir, "D2R.exe"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	last := make([]byte, 1)
	if _, err := file.ReadAt(last, info.Size()-1); err != nil {
		t.Fatal(err)
	}
	last[0] ^= 1
	if _, err := file.WriteAt(last, info.Size()-1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyGameBuild(copyDir); err != nil {
		t.Fatalf("Unrelated file change was rejected: %v", err)
	}
}
