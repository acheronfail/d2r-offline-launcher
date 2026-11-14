//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// The game process inherits a private network namespace. The installer can
// exit after launch; no process needs to wait for D2R to finish.
func offlineRuntimeCommand(runtime string, args ...string) (*exec.Cmd, error) {
	// The integration harness already supplies a network namespace and runs
	// launch() from the Go test executable, whose main cannot handle this flag.
	if os.Getenv("D2R_CAMPING_INTEGRATION") == "1" || os.Getenv("D2R_CAMPING_DETACHED_INTEGRATION") == "1" {
		return exec.Command(runtime, args...), nil
	}
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	uid, gid := os.Getuid(), os.Getgid()
	cmd := exec.Command(self, append([]string{"--offline-child", runtime}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:                 syscall.CLONE_NEWUSER | syscall.CLONE_NEWNET,
		Setsid:                     true,
		UidMappings:                []syscall.SysProcIDMap{{ContainerID: uid, HostID: uid, Size: 1}},
		GidMappings:                []syscall.SysProcIDMap{{ContainerID: gid, HostID: gid, Size: 1}},
		GidMappingsEnableSetgroups: false,
		AmbientCaps:                []uintptr{unix.CAP_NET_ADMIN},
	}
	return cmd, nil
}

func offlineChild(runtime string, args []string) error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return fmt.Errorf("Create loopback socket: %w", err)
	}
	defer unix.Close(fd)
	iface, err := unix.NewIfreq("lo")
	if err != nil {
		return err
	}
	if err := unix.IoctlIfreq(fd, unix.SIOCGIFFLAGS, iface); err != nil {
		return fmt.Errorf("Read loopback flags: %w", err)
	}
	iface.SetUint16(iface.Uint16() | unix.IFF_UP)
	if err := unix.IoctlIfreq(fd, unix.SIOCSIFFLAGS, iface); err != nil {
		return fmt.Errorf("Enable loopback: %w", err)
	}
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0); err != nil {
		return fmt.Errorf("Drop network setup capability: %w", err)
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	data := [2]unix.CapUserData{}
	if err := unix.Capset(&header, &data[0]); err != nil {
		return fmt.Errorf("Drop network setup capability: %w", err)
	}
	return syscall.Exec(runtime, append([]string{runtime}, args...), os.Environ())
}

func reportOfflineChildError(err error) {
	if err == nil {
		return
	}
	file, openErr := os.OpenFile(filepath.Join(stateDir(), "patch.log"), os.O_APPEND|os.O_WRONLY, 0600)
	if openErr != nil {
		return
	}
	fmt.Fprintf(file, "offline network setup failed: %v\n", err)
	file.Close()
}
