//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func steamRunning() bool {
	if os.Getenv("D2R_CAMPING_SHORTCUTS_FILE") != "" {
		return false // isolated test fixture, never touch the user's Steam client
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name()[0] < '0' || entry.Name()[0] > '9' {
			continue
		}
		comm, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
		if err == nil && strings.TrimSpace(string(comm)) == "steam" {
			return true
		}
	}
	return false
}

func applyOrScheduleShortcut(state string, m manifest) error {
	if steamRunning() {
		return scheduleSteamTransition(state, "install")
	}
	return activateShortcut(state, m)
}

func restoreOrScheduleShortcut(state string, m manifest) (bool, error) {
	if steamRunning() {
		return true, scheduleSteamTransition(state, "uninstall")
	}
	return false, restoreShortcut(state, m)
}

func scheduleSteamTransition(state, action string) error {
	if action != "install" && action != "uninstall" {
		return errors.New("Invalid Steam transition")
	}
	pending := filepath.Join(state, "steam-transition-pending")
	_ = os.Remove(filepath.Join(state, "steam-transition-error.txt"))
	if err := os.WriteFile(pending, []byte(action), 0600); err != nil {
		return err
	}
	unit := fmt.Sprintf("d2r-camping-%d-%d", os.Getpid(), time.Now().Unix())
	args := []string{"--user", "--collect", "--unit=" + unit}
	for _, name := range []string{"HOME", "PATH", "DISPLAY", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS", "XAUTHORITY", "XDG_CURRENT_DESKTOP", "D2R_CAMPING_STATE_DIR", "D2R_CAMPING_SHORTCUTS_FILE"} {
		if value, ok := os.LookupEnv(name); ok {
			args = append(args, "--setenv="+name+"="+value)
		}
	}
	args = append(args, installedBinaryPath(state), "--finish-steam-transition", action)
	out, err := exec.Command("systemd-run", args...).CombinedOutput()
	if err != nil {
		_ = os.Remove(pending)
		return fmt.Errorf("Could not schedule the Steam shortcut change: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func finishSteamTransition(action string) error {
	state := stateDir()
	if action != "install" && action != "uninstall" {
		return errors.New("Invalid Steam transition")
	}
	if steamRunning() {
		out, err := exec.Command("steam", "-shutdown").CombinedOutput()
		if err != nil {
			return fmt.Errorf("Could not close Steam: %s: %w", strings.TrimSpace(string(out)), err)
		}
	}
	deadline := time.Now().Add(60 * time.Second)
	for steamRunning() && time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
	}
	if steamRunning() {
		return errors.New("Steam did not close within 60 seconds; shortcut was left unchanged")
	}
	if action == "install" {
		m, err := readManifest(state)
		if err != nil {
			return err
		}
		if err := activateShortcut(state, m); err != nil {
			return err
		}
		_ = os.Remove(filepath.Join(state, "steam-transition-pending"))
	} else {
		m, err := readManifest(state)
		if err != nil {
			return err
		}
		if err := restoreTerrorZones(state, m); err != nil {
			return err
		}
		if err := restoreShortcut(state, m); err != nil {
			return err
		}
		if err := os.RemoveAll(state); err != nil {
			return err
		}
	}
	return nil
}

func recordSteamTransitionFailure(err error) {
	if err == nil {
		return
	}
	_ = os.WriteFile(filepath.Join(stateDir(), "steam-transition-error.txt"), []byte(err.Error()), 0600)
}
