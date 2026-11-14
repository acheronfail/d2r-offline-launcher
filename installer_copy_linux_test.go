//go:build linux

package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCopyInstallerAfterRunningFileIsRemoved(t *testing.T) {
	if os.Getenv("D2R_OFFLINE_COPY_TEST_CHILD") == "1" {
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(exe); err != nil {
			t.Fatal(err)
		}
		if err := copyInstaller(os.Getenv("D2R_OFFLINE_COPY_TEST_GAME")); err != nil {
			t.Fatal(err)
		}
		return
	}

	state := t.TempDir()
	game := filepath.Join(state, "game")
	if err := os.Mkdir(game, 0700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	child := filepath.Join(state, "running-test")
	out, err := os.OpenFile(child, os.O_CREATE|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(child, "-test.run=^TestCopyInstallerAfterRunningFileIsRemoved$")
	cmd.Env = append(os.Environ(), "D2R_OFFLINE_COPY_TEST_CHILD=1", "D2R_OFFLINE_COPY_TEST_GAME="+game)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Unlinked launcher copy failed: %v\n%s", err, output)
	}
	if info, err := os.Stat(installedBinaryPath(game)); err != nil || info.Size() == 0 {
		t.Fatalf("Reinstalled launcher is missing: %v", err)
	}
}
