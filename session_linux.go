//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// Locks are held for the whole session and released by the kernel even on a crash.
// Locking the executable also prevents sessions using different state directories
// from modifying the same game's data concurrently.
func lockFile(path string, create bool) (*os.File, error) {
	flags := os.O_RDONLY
	if create {
		flags = os.O_CREATE | os.O_RDWR
	}
	f, err := os.OpenFile(path, flags, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("Another launcher session is active: %w", err)
	}
	return f, nil
}

func lockState() (*os.File, error) {
	state := stateDir()
	if s, err := os.Lstat(state); err == nil && (!s.IsDir() || s.Mode()&os.ModeSymlink != 0) {
		return nil, errors.New("Settings path is unsafe")
	}
	if err := os.MkdirAll(state, 0700); err != nil {
		return nil, err
	}
	return lockFile(filepath.Join(state, "session.lock"), true)
}

func finishSession(state string, m manifest) error {
	if gameRunningInPrefix(m.Prefix) {
		return errors.New("D2R is still running; cleanup will resume after it exits")
	}
	if err := restoreTerrorZones(state, m); err != nil {
		return err
	}
	// Delete the record first: an orphan backup can safely be discarded, whereas
	// a record without its backup must always stop recovery.
	for _, name := range []string{terrorZoneRecordName, terrorZoneBackupName} {
		if err := os.Remove(filepath.Join(state, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := syncDirectory(state); err != nil {
			return err
		}
	}
	return nil
}

// Called with the state lock held, before settings can be changed.
func recoverPending() error {
	state := stateDir()
	_, recordErr := os.Stat(filepath.Join(state, terrorZoneRecordName))
	_, backupErr := os.Stat(filepath.Join(state, terrorZoneBackupName))
	if errors.Is(recordErr, os.ErrNotExist) && errors.Is(backupErr, os.ErrNotExist) {
		return nil
	}
	m, err := readManifest(state)
	if err != nil {
		return fmt.Errorf("Read settings for interrupted session: %w", err)
	}
	lock, err := lockFile(filepath.Join(m.Game, "D2R.exe"), false)
	if err != nil {
		return err
	}
	defer lock.Close()
	return finishSession(state, m)
}

func recoverOnStartup() error {
	lock, err := lockState()
	if err != nil {
		return err
	}
	defer lock.Close()
	return recoverPending()
}

func runSession(game, prefix string, started func()) (result error) {
	lock, err := lockState()
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := recoverPending(); err != nil {
		return err
	}
	gameLock, err := lockFile(filepath.Join(game, "D2R.exe"), false)
	if err != nil {
		return err
	}
	defer gameLock.Close()
	if err := saveSettings(game, prefix); err != nil {
		return err
	}
	m, err := readManifest(stateDir())
	if err != nil {
		return err
	}
	if gameRunningInPrefix(m.Prefix) {
		return errors.New("Close D2R before starting another offline session")
	}
	library, err := payload.ReadFile("payload/license.so")
	if err != nil {
		return err
	}
	if err := atomicGameFile(filepath.Join(stateDir(), "license.so"), library, 0600); err != nil {
		return err
	}
	defer func() {
		// A startup timeout is not proof that the game stopped. Never restore data
		// underneath a live process, including one that started without confirmation.
		for gameRunningInPrefix(m.Prefix) {
			time.Sleep(250 * time.Millisecond)
		}
		result = errors.Join(result, finishSession(stateDir(), m))
	}()
	return launchStep(started)
}

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// pidfds keep the identity stable even if Linux reuses the numeric PID.
func waitForGame(pid int, started func()) error {
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return fmt.Errorf("Track D2R process: %w", err)
	}
	defer unix.Close(fd)
	started()
	for {
		_, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, -1)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return err
	}
}
