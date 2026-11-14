//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func canonicalPrefix(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

func prefixProcess(pid int, wanted string) (string, []byte, bool) {
	base := filepath.Join("/proc", strconv.Itoa(pid))
	environ, err := os.ReadFile(filepath.Join(base, "environ"))
	if err != nil {
		return "", nil, false
	}
	match := false
	for _, entry := range bytes.Split(environ, []byte{0}) {
		if value, found := bytes.CutPrefix(entry, []byte("STEAM_COMPAT_DATA_PATH=")); found &&
			canonicalPrefix(string(value)) == wanted {
			match = true
			break
		}
	}
	if !match {
		return "", nil, false
	}
	comm, err := os.ReadFile(filepath.Join(base, "comm"))
	if err != nil {
		return "", nil, false
	}
	cmdline, _ := os.ReadFile(filepath.Join(base, "cmdline"))
	return strings.TrimSpace(string(comm)), cmdline, true
}

func gameRunningInPrefix(prefix string) bool {
	wanted := canonicalPrefix(prefix)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		comm, _, match := prefixProcess(pid, wanted)
		if match && (comm == "Main" || comm == "D2R.exe") {
			return true
		}
	}
	return false
}

func preparePrefixForOfflineLaunch(prefix, proton string) error {
	wanted := canonicalPrefix(prefix)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	background := map[string]bool{
		"steam.exe": true, "wineserver": true, "services.exe": true,
		"winedevice.exe": true, "plugplay.exe": true, "svchost.exe": true,
		"explorer.exe": true, "rpcss.exe": true, "tabtip.exe": true,
		"xalia.exe": true, "conhost.exe": true, "D2R-Offline-Lau": true,
	}
	var staleLaunchers []int
	serverPresent := false
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		comm, cmdline, match := prefixProcess(pid, wanted)
		if !match {
			continue
		}
		if comm == "python3" && bytes.Contains(cmdline, []byte("/proton")) &&
			bytes.Contains(cmdline, []byte("D2R.exe")) {
			staleLaunchers = append(staleLaunchers, pid)
			continue
		}
		if !background[comm] {
			return fmt.Errorf("Close %s in this Proton prefix before launching offline", comm)
		}
		if comm == "wineserver" || comm == "steam.exe" || comm == "services.exe" {
			serverPresent = true
		}
	}
	// A previous detached Proton launcher can linger behind its Wine server
	// after the game closes. Stop only our D2R launchers before ending that
	// idle server, so they cannot start a queued second game during cleanup.
	for _, pid := range staleLaunchers {
		comm, cmdline, match := prefixProcess(pid, wanted)
		if !match || comm != "python3" || !bytes.Contains(cmdline, []byte("D2R.exe")) {
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
	}
	for _, pid := range staleLaunchers {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid))); os.IsNotExist(err) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid))); err == nil {
			return errors.New("A previous D2R Proton launcher is still running; close it before trying again")
		}
	}
	if !serverPresent && len(staleLaunchers) == 0 {
		return nil
	}
	wineserver := filepath.Join(filepath.Dir(proton), "files", "bin", "wineserver")
	if _, err := os.Stat(wineserver); err != nil {
		return fmt.Errorf("Find Proton wineserver: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, wineserver, "-k")
	command.Env = append(os.Environ(), "WINEPREFIX="+filepath.Join(prefix, "pfx"))
	if out, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("Close idle Proton services: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}
