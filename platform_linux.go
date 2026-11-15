//go:build linux

package main

import (
	"context"
	"debug/pe"
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

func verifyGameBuild(game string) error {
	file, err := pe.Open(filepath.Join(game, "D2R.exe"))
	if err != nil {
		return errors.New("D2R.exe is not a readable Windows executable")
	}
	defer file.Close()
	if file.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return errors.New("D2R.exe must be a 64-bit Windows executable")
	}
	sections := map[string]bool{".text": false, ".rdata": false, ".data": false}
	for _, section := range file.Sections {
		if _, needed := sections[section.Name]; needed {
			sections[section.Name] = true
		}
	}
	for _, present := range sections {
		if !present {
			return errors.New("D2R.exe lacks the expected code and data sections")
		}
	}
	return nil
}

func steamRoots() []string {
	h, _ := os.UserHomeDir()
	return []string{filepath.Join(h, ".steam", "steam"), filepath.Join(h, ".local", "share", "Steam")}
}
func choosePrefix() string {
	if p := os.Getenv("D2R_OFFLINE_PREFIX"); p != "" {
		return p
	}
	if game := autodetect(); game != "" {
		if p := shortcutPrefix(game); p != "" {
			return p
		}
	}
	best := ""
	score := -1
	for _, root := range steamRoots() {
		entries, _ := os.ReadDir(filepath.Join(root, "steamapps", "compatdata"))
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if _, e := strconv.ParseUint(entry.Name(), 10, 32); e != nil {
				continue
			}
			p := filepath.Join(root, "steamapps", "compatdata", entry.Name())
			s := 0
			if _, e := os.Stat(cookiePath(p)); e == nil {
				s += 100
			}
			if _, e := os.Stat(filepath.Join(p, "pfx", "drive_c", "ProgramData", "Battle.net", "Agent")); e == nil {
				s += 10
			}
			if data, e := os.ReadFile(filepath.Join(p, "pfx", "user.reg")); e == nil &&
				strings.Contains(string(data), "Diablo II Resurrected") {
				s += 5
			}
			files, _ := os.ReadDir(filepath.Join(p, "pfx", "drive_c", "users", "steamuser", "Saved Games", "Diablo II Resurrected"))
			for _, f := range files {
				if strings.HasSuffix(strings.ToLower(f.Name()), ".d2s") {
					s++
				}
			}
			if s > score && s > 0 {
				best = p
				score = s
			}
		}
	}
	return best
}
func protonPaths(prefix string) (string, string, string, error) {
	configured := configuredProton(prefix)
	wanted, _ := os.ReadFile(filepath.Join(prefix, "version"))
	major := ""
	if match := prefixMajorPattern.FindSubmatch(wanted); len(match) == 2 {
		major = string(match[1])
	}
	bestScore := -1
	var bestRoot, bestProton, bestRuntime string
	for _, root := range steamRoots() {
		dir := filepath.Join(root, "steamapps", "common")
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), "Proton") {
				continue
			}
			if configured != "" && entry.Name() != configured {
				continue
			}
			proton := filepath.Join(dir, entry.Name(), "proton")
			runtime := filepath.Join(dir, "SteamLinuxRuntime_sniper", "run")
			if _, e := os.Stat(proton); e != nil {
				continue
			}
			if _, e := os.Stat(runtime); e == nil {
				score := 0
				version, _ := os.ReadFile(filepath.Join(dir, entry.Name(), "version"))
				if major != "" {
					if match := protonMajorPattern.FindSubmatch(version); len(match) == 2 && string(match[1]) == major {
						score += 10
					}
				}
				if !strings.Contains(entry.Name(), "Experimental") {
					score++
				}
				if score > bestScore || score == bestScore && entry.Name() > filepath.Base(filepath.Dir(bestProton)) {
					bestScore, bestRoot, bestProton, bestRuntime = score, root, proton, runtime
				}
			}
		}
	}
	if bestScore >= 0 {
		return bestRoot, bestProton, bestRuntime, nil
	}
	if configured != "" {
		return "", "", "", fmt.Errorf("Steam's configured %s is missing; install it in Steam", configured)
	}
	return "", "", "", errors.New("Proton and Steam Linux Runtime Sniper were not found")
}

func gpuEnvironment() []string {
	if _, userChoice := os.LookupEnv("VKD3D_FILTER_DEVICE_NAME"); userChoice {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=name", "--format=csv,noheader").Output()
	if err != nil {
		return nil
	}
	name := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if !strings.Contains(name, "NVIDIA") {
		return nil
	}
	return []string{"__NV_PRIME_RENDER_OFFLOAD=1", "__VK_LAYER_NV_optimus=NVIDIA_only",
		"__GLX_VENDOR_LIBRARY_NAME=nvidia", "VKD3D_FILTER_DEVICE_NAME=" + name,
		"DXVK_FILTER_DEVICE_NAME=" + name}
}

func launch(started func()) (result error) {
	state := stateDir()
	m, e := readManifest(state)
	if e != nil {
		return e
	}
	offset, _, e := offlineClockOffset(m.Prefix, time.Now())
	if e != nil {
		return e
	}
	if e := verifyGameBuild(m.Game); e != nil {
		return e
	}
	root, proton, runtime, e := protonPaths(m.Prefix)
	if e != nil {
		return e
	}
	if e := preparePrefixForOfflineLaunch(m.Prefix, proton); e != nil {
		return e
	}
	if e := alignTerrorZones(state, m, time.Now(), offset); e != nil {
		return e
	}
	licensePatch := filepath.Join(state, "license.so")
	if _, e := os.Stat(licensePatch); e != nil {
		return errors.New("Offline support library is missing; try Launch Offline again")
	}
	patchLog := filepath.Join(state, "patch.log")
	if e := os.WriteFile(patchLog, nil, 0600); e != nil {
		return e
	}
	preload := licensePatch
	if inherited := os.Getenv("LD_PRELOAD"); inherited != "" {
		preload += ":" + inherited
	}
	appid := m.ShortcutAppID
	if appid == "" {
		appid = shortcutAppID(m.Game)
	}
	if appid == "" {
		appid = filepath.Base(m.Prefix)
	}
	if override := os.Getenv("D2R_OFFLINE_STEAM_APP_ID"); override != "" {
		appid = override
	}
	args := []string{"--", "/usr/bin/env", "STEAM_COMPAT_DATA_PATH=" + m.Prefix,
		"STEAM_COMPAT_CLIENT_INSTALL_PATH=" + root, "SteamAppId=" + appid,
		"SteamGameId=" + appid, "LD_PRELOAD=" + preload,
		"D2R_OFFLINE_CLOCK_OFFSET_SECONDS=" + strconv.FormatInt(offset, 10),
		"D2R_ELF_PATCH_LOG=" + patchLog}
	args = append(args, gpuEnvironment()...)
	args = append(args, proton, "waitforexitandrun", filepath.Join(m.Game, "D2R.exe"))
	args = append(args, gameArgsFromOptions(m.LaunchOptions)...)
	cmd, e := offlineRuntimeCommand(runtime, args...)
	if e != nil {
		return e
	}
	cmd.Dir = m.Game
	cmd.Env = os.Environ()
	log, e := os.OpenFile(filepath.Join(state, "last-launch.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	cmd.Stdout = log
	cmd.Stderr = log
	e = cmd.Start()
	log.Close()
	if e != nil {
		return e
	}
	runtimeDone := make(chan struct{})
	go func() { _ = cmd.Wait(); close(runtimeDone) }()
	defer func() {
		if result == nil {
			return
		}
		// Stop this launch attempt before recovery. A timeout must not leave
		// a delayed runtime free to start D2R after its data has been restored.
		select {
		case <-runtimeDone:
			return
		default:
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-runtimeDone:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-runtimeDone
		}
	}()
	confirmation := "clock shift active; seconds=" + strconv.FormatInt(offset, 10)
	deadline := time.Now().Add(75 * time.Second)
	for time.Now().Before(deadline) {
		if log, e := os.ReadFile(patchLog); e == nil {
			message := string(log)
			if strings.Contains(message, confirmation) {
				for _, line := range strings.Split(message, "\n") {
					if !strings.Contains(line, confirmation) {
						continue
					}
					fields := strings.Fields(line)
					if len(fields) == 0 || !strings.HasPrefix(fields[0], "pid=") {
						return errors.New("Offline clock shim did not report the game process ID")
					}
					pid, err := strconv.Atoi(strings.TrimPrefix(fields[0], "pid="))
					if err != nil || pid <= 0 {
						return errors.New("Invalid D2R process ID")
					}
					return waitForGame(pid, started)
				}
			}
			if strings.Contains(message, "invalid offline clock offset") || strings.Contains(message, "unsupported D2R PE image") || strings.Contains(message, "offline network setup failed") {
				return fmt.Errorf("Offline clock shift was not applied: %s", strings.TrimSpace(message))
			}
		}
		select {
		case <-runtimeDone:
			return errors.New("D2R exited before confirming startup; check last-launch.log")
		case <-time.After(250 * time.Millisecond):
		}
	}
	return errors.New("D2R did not confirm its process-only clock shift within 75 seconds; check last-launch.log and use Battle.net normally if this build is unsupported")
}
