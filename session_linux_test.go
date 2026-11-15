//go:build linux

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixtureSession(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("D2R_OFFLINE_STATE_DIR", filepath.Join(home, "state"))
	game, prefix := filepath.Join(home, "game"), filepath.Join(home, "prefix")
	for _, dir := range []string{game, filepath.Join(prefix, "pfx")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"D2R.exe", "D2R_loader.dll"} {
		if err := os.WriteFile(filepath.Join(game, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return game, prefix
}

func fixtureZones(t *testing.T, game string) []byte {
	t.Helper()
	original := []byte(`{"desecrated_zones":[{"start_time_utc":"2025-12-05 00:00:00","end_time_utc":"","seed":12345,"zone_duration_minutes":30,"break_duration_minutes":0,"zones":[{"id":"a"},{"id":"b"}]}]}`)
	path := terrorZonePath(game)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	return original
}

func setDirect(t *testing.T) manifest {
	t.Helper()
	m, err := readManifest(stateDir())
	if err != nil {
		t.Fatal(err)
	}
	m.LaunchOptions = "%command% -direct -txt"
	data, _ := json.Marshal(m)
	if err := atomicGameFile(filepath.Join(stateDir(), "settings.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSessionRestoresOnExitAndLaunchFailure(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "exit", true: "failed_launch"}[failure], func(t *testing.T) {
			allowFixtureBuild(t)
			game, prefix := fixtureSession(t)
			original := fixtureZones(t, game)
			if err := saveSettings(game, prefix); err != nil {
				t.Fatal(err)
			}
			m := setDirect(t)
			previous := launchStep
			defer func() { launchStep = previous }()
			launchStep = func(ready func()) error {
				if err := alignTerrorZones(stateDir(), m, time.Now(), -86400); err != nil {
					return err
				}
				generated, _ := os.ReadFile(terrorZonePath(game))
				if bytes.Equal(generated, original) {
					t.Error("No session data generated")
				}
				if err := recoverOnStartup(); err == nil {
					t.Error("Concurrent startup acquired session lock")
				}
				if err := runSession(game, prefix, func() {}); err == nil {
					t.Error("Concurrent launch succeeded")
				}
				ready()
				if failure {
					return errors.New("simulated launch failure")
				}
				return nil
			}
			err := runSession(game, prefix, func() {})
			if (err != nil) != failure {
				t.Fatalf("Unexpected result: %v", err)
			}
			restored, _ := os.ReadFile(terrorZonePath(game))
			if !bytes.Equal(restored, original) {
				t.Fatal("Original not restored")
			}
			for _, name := range []string{terrorZoneBackupName, terrorZoneRecordName, "installation.json", "selection.json"} {
				if _, err := os.Stat(filepath.Join(stateDir(), name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("Leftover %s: %v", name, err)
				}
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(game), "D2R-Offline-Launcher")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("Launcher copied itself")
			}
		})
	}
}

func TestStartupRecovery(t *testing.T) {
	for _, stage := range []string{"backup_only", "record_before_replacement", "generated", "external_edit", "damaged_backup", "missing_backup", "corrupt_record", "wrong_path", "missing_game"} {
		t.Run(stage, func(t *testing.T) {
			allowFixtureBuild(t)
			game, prefix := fixtureSession(t)
			original := fixtureZones(t, game)
			if err := saveSettings(game, prefix); err != nil {
				t.Fatal(err)
			}
			m := setDirect(t)
			if err := prepareTerrorZones(stateDir(), m); err != nil {
				t.Fatal(err)
			}
			if stage != "backup_only" {
				if err := alignTerrorZones(stateDir(), m, time.Now(), -86400); err != nil {
					t.Fatal(err)
				}
			}
			switch stage {
			case "record_before_replacement":
				os.WriteFile(terrorZonePath(game), original, 0644)
			case "external_edit":
				os.WriteFile(terrorZonePath(game), []byte("external edit"), 0644)
			case "damaged_backup":
				os.WriteFile(filepath.Join(stateDir(), terrorZoneBackupName), []byte("bad"), 0600)
			case "missing_backup":
				os.Remove(filepath.Join(stateDir(), terrorZoneBackupName))
			case "corrupt_record":
				os.WriteFile(filepath.Join(stateDir(), terrorZoneRecordName), []byte("broken JSON"), 0600)
			case "wrong_path":
				record, err := readTerrorZoneRecord(stateDir())
				if err != nil {
					t.Fatal(err)
				}
				record.Path = filepath.Join(t.TempDir(), "other.json")
				data, _ := json.Marshal(record)
				os.WriteFile(filepath.Join(stateDir(), terrorZoneRecordName), data, 0600)
			case "missing_game":
				os.Remove(terrorZonePath(game))
			}
			err := recoverOnStartup()
			wantError := stage == "damaged_backup" || stage == "missing_backup" || stage == "external_edit" || stage == "corrupt_record" || stage == "wrong_path" || stage == "missing_game"
			if (err != nil) != wantError {
				t.Fatalf("Recovery: %v", err)
			}
			got, _ := os.ReadFile(terrorZonePath(game))
			if stage == "external_edit" {
				if string(got) != "external edit" {
					t.Fatal("Overwrote external change")
				}
			}
			if !wantError && !bytes.Equal(got, original) {
				t.Fatal("Original not recovered")
			}
			if wantError {
				if _, err := os.Stat(filepath.Join(stateDir(), terrorZoneRecordName)); err != nil {
					t.Fatal("Lost recovery record")
				}
			}
			if !wantError {
				if err := recoverOnStartup(); err != nil {
					t.Fatalf("Repeated recovery failed: %v", err)
				}
			}
		})
	}
}

func TestGameLockAcrossStateDirectories(t *testing.T) {
	game, _ := fixtureSession(t)
	lock, err := lockFile(filepath.Join(game, "D2R.exe"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(game, alias); err != nil {
		t.Fatal(err)
	}
	if other, err := lockFile(filepath.Join(alias, "D2R.exe"), false); err == nil {
		other.Close()
		t.Fatal("Game alias bypassed lock")
	}
}

func TestProcessTrackingExitAndCrash(t *testing.T) {
	for _, crash := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "crash"}[crash], func(t *testing.T) {
			cmd := exec.Command("sleep", "30")
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { cmd.Process.Kill(); cmd.Wait() }()
			started, done := make(chan struct{}), make(chan error, 1)
			go func() { done <- waitForGame(cmd.Process.Pid, func() { close(started) }) }()
			select {
			case <-started:
			case err := <-done:
				t.Fatal(err)
			case <-time.After(time.Second):
				t.Fatal("No process tracking")
			}
			select {
			case err := <-done:
				t.Fatalf("Returned before exit: %v", err)
			default:
			}
			if crash {
				cmd.Process.Kill()
			} else {
				cmd.Process.Signal(os.Interrupt)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("Did not detect exit")
			}
		})
	}
}

func TestLegacySettingsMigration(t *testing.T) {
	allowFixtureBuild(t)
	game, prefix := fixtureSession(t)
	os.MkdirAll(stateDir(), 0700)
	data, _ := json.Marshal(manifest{Game: game, Prefix: prefix, Version: 4, LaunchOptions: "-direct -txt", ShortcutAppID: "123"})
	os.WriteFile(filepath.Join(stateDir(), "installation.json"), data, 0600)
	if err := saveSettings(game, ""); err != nil {
		t.Fatal(err)
	}
	m, err := readManifest(stateDir())
	if err != nil || m.Prefix != prefix || m.ShortcutAppID != "123" || m.LaunchOptions != "-direct -txt" {
		t.Fatalf("Migration lost settings: %+v %v", m, err)
	}
}

func TestInvalidBuildAndUnsafeState(t *testing.T) {
	game, prefix := fixtureSession(t)
	if err := saveSettings(game, prefix); err == nil || !strings.Contains(err.Error(), "Windows executable") {
		t.Fatalf("Invalid executable accepted: %v", err)
	}
	alias := filepath.Join(t.TempDir(), "state")
	os.Symlink(t.TempDir(), alias)
	t.Setenv("D2R_OFFLINE_STATE_DIR", alias)
	if lock, err := lockState(); err == nil {
		lock.Close()
		t.Fatal("Symlink state accepted")
	}
}

func TestInterruptedSessionRecoversAfterLauncherKilled(t *testing.T) {
	if os.Getenv("D2R_TEST_CRASH_CHILD") == "1" {
		verifySelectedBuild = func(string) error { return nil }
		game, prefix := os.Getenv("D2R_TEST_GAME"), os.Getenv("D2R_TEST_PREFIX")
		launchStep = func(func()) error {
			m, err := readManifest(stateDir())
			if err != nil {
				return err
			}
			if err := alignTerrorZones(stateDir(), m, time.Now(), -86400); err != nil {
				return err
			}
			os.Stdout.WriteString("session-ready\n")
			select {}
		}
		if err := runSession(game, prefix, func() {}); err != nil {
			t.Fatal(err)
		}
		return
	}
	allowFixtureBuild(t)
	game, prefix := fixtureSession(t)
	original := fixtureZones(t, game)
	if err := saveSettings(game, prefix); err != nil {
		t.Fatal(err)
	}
	setDirect(t)
	child := exec.Command(os.Args[0], "-test.run=^TestInterruptedSessionRecoversAfterLauncherKilled$")
	child.Env = append(os.Environ(), "D2R_TEST_CRASH_CHILD=1", "D2R_TEST_GAME="+game, "D2R_TEST_PREFIX="+prefix)
	out, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { child.Process.Kill(); child.Wait() }()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(out).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "session-ready\n" {
			t.Fatalf("Child failed: %q", line)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Child failed to prepare data")
	}
	if err := recoverOnStartup(); err == nil {
		t.Fatal("Recovered while child owned session")
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	child.Wait()
	got, _ := os.ReadFile(terrorZonePath(game))
	if bytes.Equal(got, original) {
		t.Fatal("Forced kill unexpectedly restored data")
	}
	if err := recoverOnStartup(); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(terrorZonePath(game))
	if !bytes.Equal(got, original) {
		t.Fatal("Startup did not recover terminated session")
	}
}

func TestFailedStartupWaitsForLiveGameBeforeRestoring(t *testing.T) {
	allowFixtureBuild(t)
	game, prefix := fixtureSession(t)
	original := fixtureZones(t, game)
	if err := saveSettings(game, prefix); err != nil {
		t.Fatal(err)
	}
	m := setDirect(t)
	command := exec.Command("python3", "-c", "import ctypes,time; ctypes.CDLL(None).prctl(15,b'Main',0,0,0); print('ready',flush=True); time.sleep(30)")
	command.Env = append(os.Environ(), "STEAM_COMPAT_DATA_PATH="+prefix)
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	previous := launchStep
	defer func() { launchStep = previous }()
	launchStep = func(func()) error {
		if err := alignTerrorZones(stateDir(), m, time.Now(), -86400); err != nil {
			return err
		}
		if err := command.Start(); err != nil {
			return err
		}
		line, _ := bufio.NewReader(output).ReadString('\n')
		if line != "ready\n" {
			return errors.New("fake game did not start")
		}
		close(started)
		return errors.New("simulated confirmation timeout")
	}
	defer func() {
		if command.Process != nil {
			command.Process.Kill()
			command.Wait()
		}
	}()
	done := make(chan error, 1)
	go func() { done <- runSession(game, prefix, func() {}) }()
	select {
	case <-started:
	case err := <-done:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("Game did not start")
	}
	select {
	case err := <-done:
		t.Fatalf("Restored under live game: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	current, _ := os.ReadFile(terrorZonePath(game))
	if bytes.Equal(current, original) {
		t.Error("Restored while game was alive")
	}
	command.Process.Kill()
	command.Wait()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Lost launch failure")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Cleanup did not resume")
	}
	current, _ = os.ReadFile(terrorZonePath(game))
	if !bytes.Equal(current, original) {
		t.Fatal("Did not restore after late game exit")
	}
}
