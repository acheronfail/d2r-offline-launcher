//go:build linux

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Opt-in: uses a disposable copy of the Proton prefix, but the real game's
// extracted JSON. Always leave recovery artifacts on disk if a live test fails.
func TestLiveGameSession(t *testing.T) {
	game, prefix := os.Getenv("D2R_OFFLINE_LIVE_GAME"), os.Getenv("D2R_OFFLINE_LIVE_PREFIX")
	if game == "" || prefix == "" {
		t.Skip("set D2R_OFFLINE_LIVE_GAME and D2R_OFFLINE_LIVE_PREFIX for live D2R testing")
	}
	binary := filepath.Join(t.TempDir(), "D2R-Offline-Launcher")
	if out, err := exec.Command("go", "build", "-buildvcs=false", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("Build: %v %s", err, out)
	}
	closeWindow := filepath.Join(t.TempDir(), "close-game-window")
	compiler := exec.Command("gcc", "-x", "c", "-", "-o", closeWindow, "-lX11")
	compiler.Stdin = strings.NewReader(closeGameWindowSource)
	if out, err := compiler.CombinedOutput(); err != nil {
		t.Fatalf("Build window helper: %v %s", err, out)
	}
	for _, mode := range []string{"window_close", "game_crash", "launcher_crash"} {
		t.Run(mode, func(t *testing.T) {
			state, err := os.MkdirTemp("/tmp", "d2r-live-"+mode+"-")
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("D2R_OFFLINE_STATE_DIR", state)
			t.Logf("Recovery files: %s", state)
			original, err := os.ReadFile(terrorZonePath(game))
			if err != nil {
				t.Fatal(err)
			}
			before := map[string][32]byte{}
			for _, p := range []string{filepath.Join(game, "D2R.exe"), filepath.Join(game, "D2R_loader.dll"), cookiePath(prefix)} {
				data, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				before[p] = sha256.Sum256(data)
			}
			if err := saveSettings(game, prefix); err != nil {
				t.Fatal(err)
			}
			m, err := readManifest(state)
			if err != nil {
				t.Fatal(err)
			}
			m.LaunchOptions = "%command% -direct -txt"
			m.ShortcutAppID = "4292372109"
			data, _ := json.Marshal(m)
			if err := atomicGameFile(filepath.Join(state, "settings.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			command := exec.Command(binary, "--launch-offline")
			command.Env = os.Environ()
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			defer command.Process.Kill()
			go func() { done <- command.Wait() }()
			deadline := time.Now().Add(100 * time.Second)
			confirmed := false
			for time.Now().Before(deadline) {
				log, _ := os.ReadFile(filepath.Join(state, "patch.log"))
				if bytes.Contains(log, []byte("clock shift active")) {
					confirmed = true
					break
				}
				select {
				case err := <-done:
					t.Fatalf("Launch failed: %v (logs in %s)", err, state)
				case <-time.After(250 * time.Millisecond):
				}
			}
			if !confirmed {
				t.Fatal("Launch timed out; recovery files retained")
			}

			log, err := os.ReadFile(filepath.Join(state, "patch.log"))
			if err != nil {
				t.Fatal(err)
			}
			match := regexp.MustCompile(`pid=(\d+) clock shift active`).FindSubmatch(log)
			if len(match) != 2 {
				t.Fatal("No game PID")
			}
			pid, _ := strconv.Atoi(string(match[1]))
			t.Logf("Tracking D2R PID %d", pid)
			fd, err := unix.PidfdOpen(pid, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0); unix.Close(fd) }()
			generated, err := os.ReadFile(terrorZonePath(game))
			if err != nil || bytes.Equal(generated, original) {
				t.Fatalf("No aligned game data: %v", err)
			}
			// Give the renderer time to display its window, also proving the session stays open.
			select {
			case err := <-done:
				t.Fatalf("Session returned while game should run: %v", err)
			case <-time.After(15 * time.Second):
			}
			if mode == "launcher_crash" {
				if err := command.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-done:
					if err == nil {
						t.Fatal("Launcher unexpectedly exited normally")
					}
				case <-time.After(5 * time.Second):
					t.Fatal("Launcher did not terminate")
				}
				if err := recoverOnStartup(); err == nil {
					t.Fatal("Recovery ran underneath the orphaned game")
				}
				current, _ := os.ReadFile(terrorZonePath(game))
				if bytes.Equal(current, original) {
					t.Fatal("Restored while orphaned game was alive")
				}
				if err := unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0); err != nil {
					t.Fatal(err)
				}
				if n, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 10000); err != nil || n != 1 {
					t.Fatalf("Game did not stop: %v", err)
				}
				if err := recoverOnStartup(); err != nil {
					t.Fatal(err)
				}
			} else {
				if mode == "window_close" {
					if output, err := exec.Command(closeWindow).CombinedOutput(); err != nil {
						t.Fatalf("Close game window: %v %s", err, output)
					}
				} else if err := unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(30 * time.Second):
					t.Fatal("Session did not finish after game exit")
				}
			}

			restored, err := os.ReadFile(terrorZonePath(game))
			if err != nil || !bytes.Equal(restored, original) {
				t.Fatalf("Game data not restored: %v", err)
			}
			for p, want := range before {
				data, err := os.ReadFile(p)
				if err != nil || sha256.Sum256(data) != want {
					t.Fatalf("File changed: %s (%v)", p, err)
				}
			}
			for _, name := range []string{terrorZoneRecordName, terrorZoneBackupName} {
				if _, err := os.Stat(filepath.Join(state, name)); !os.IsNotExist(err) {
					t.Fatalf("Recovery artifact remained: %s", name)
				}
			}
			t.Log(fmt.Sprintf("%s: exact JSON restored; executable, loader and cookie unchanged", mode))
		})
	}
}

// Send the same close request as a window manager, without interacting with saves.
const closeGameWindowSource = `
#include <X11/Xlib.h>
#include <string.h>
static Window find(Display *d, Window root) {
 char *name = 0;
 if (XFetchName(d,root,&name) && name) {
  int match = strcmp(name,"Diablo II: Resurrected")==0;
  XFree(name); if(match) return root;
 }
 Window r,p,*children=0; unsigned n=0;
 if(!XQueryTree(d,root,&r,&p,&children,&n)) return 0;
 Window result=0;
 for(unsigned i=0;i<n&&!result;i++) result=find(d,children[i]);
 if(children) XFree(children);
 return result;
}
int main(void){
 Display *d=XOpenDisplay(0); if(!d) return 2;
 Window w=find(d,DefaultRootWindow(d)); if(!w) return 3;
 XEvent event={0}; event.xclient.type=ClientMessage;event.xclient.window=w;
 event.xclient.message_type=XInternAtom(d,"WM_PROTOCOLS",False);event.xclient.format=32;
 event.xclient.data.l[0]=XInternAtom(d,"WM_DELETE_WINDOW",False);event.xclient.data.l[1]=CurrentTime;
 XSendEvent(d,w,False,NoEventMask,&event);XFlush(d);XCloseDisplay(d);return 0;
}
`
