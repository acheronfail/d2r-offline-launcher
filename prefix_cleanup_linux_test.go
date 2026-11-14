//go:build linux

package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestPrefixCleanupRefusesActiveProgram(t *testing.T) {
	prefix := t.TempDir()
	command := exec.Command("sleep", "5")
	command.Env = append(os.Environ(), "STEAM_COMPAT_DATA_PATH="+prefix)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	}()
	time.Sleep(50 * time.Millisecond)
	err := preparePrefixForOfflineLaunch(prefix, "/missing/proton")
	if err == nil || !strings.Contains(err.Error(), "Close sleep") {
		t.Fatalf("active prefix program was not protected: %v", err)
	}
}
