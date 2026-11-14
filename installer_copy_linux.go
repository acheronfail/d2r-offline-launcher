//go:build linux

package main

import (
	"io"
	"os"
	"path/filepath"
)

func installedBinaryPath(game string) string {
	return filepath.Join(filepath.Dir(game), "D2R-Offline-Launcher")
}

func copyInstaller(game string) error {
	in, err := os.Open("/proc/self/exe")
	if err != nil {
		return err
	}
	defer in.Close()
	target := installedBinaryPath(game)
	if src, err := in.Stat(); err == nil {
		if dst, err := os.Stat(target); err == nil && os.SameFile(src, dst) {
			return nil
		}
	}
	out, err := os.CreateTemp(filepath.Dir(target), ".installer-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	if err = out.Chmod(0700); err == nil {
		_, err = io.Copy(out, in)
	}
	if err == nil {
		err = out.Sync()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(out.Name(), target)
}
