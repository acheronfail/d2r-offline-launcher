# D2R Offline Launcher

Single-file Linux/Proton GUI for playing Battle.net Diablo II: Resurrected offline with its cached license cookie.

## Steam Deck

1. Put `release/D2R-Offline-Launcher-Linux` somewhere permanent and mark it executable in Desktop Mode. The launcher runs from that location; it does not copy itself.
2. Open it, check the detected D2R folder and Battle.net Proton prefix, then tap **Launch Offline**. Paths and Steam launch settings are saved automatically.
3. Point the existing D2R Steam shortcut at that executable as a native Linux app and clear its old launch options. Keep the separate Battle.net shortcut for online play and updates.

There is no install or uninstall step. The launcher prepares its embedded support library under `~/.local/share/d2r-offline-launcher` when launching. Existing version 4 installation settings migrate automatically. License notices are available under **Help → Licenses**.

The launcher stays running for the game session and restores temporary Terror Zone data when D2R exits, including after a game crash. Closing the launcher window during a session hides it; it reappears when cleanup finishes. Wait for the session to finish before updating through Battle.net. If the launcher is forcibly terminated or power is lost, reopen it after D2R exits to finish recovery. Recovery preserves externally changed files and keeps backups when it cannot safely restore them. Concurrent launcher sessions cannot modify the same game's data.

Offline launches use a private network namespace and a process-only clock shift near the start of the cookie's window. The cached cookie must include your purchased-content entitlements.

With extracted game data and `-direct -txt`, offline launches temporarily align Terror Zones with real time using a seven-day schedule. The original `Data/hd/global/excel/desecratedzones.json` is backed up before modification and restored after the session. The rotation follows [Arcane Tracker's findings](https://github.com/hyperremix/d2r-arcane-tracker/blob/main/docs/TERROR_ZONE_FINDINGS_2026.md).

For command-line use, run `D2R-Offline-Launcher-Linux --launch-offline [game-folder [compatdata-folder]]`. Omitted paths use saved settings or detection. `D2R_OFFLINE_STATE_DIR` overrides the support-file directory; use the same directory between runs so interrupted sessions can be recovered.

## Build

Run `just package` to test and build the single executable in `release/`. `just test`, `just build`, and `just clean` are also available. Building requires Go, GCC, and Fyne's X11/OpenGL development libraries. The binary requires glibc 2.38 or newer.

`go test -race ./...` checks the session and GUI lifecycle for data races. Live tests are opt-in: set `D2R_OFFLINE_LIVE_GAME` to the game folder and `D2R_OFFLINE_LIVE_PREFIX` to a disposable copy of its Proton prefix, then run `go test -run '^TestLiveGameSession$' -v -timeout 5m`. These tests open D2R, close its window, kill the game, and kill the launcher while the game remains open. They verify exact restoration and unchanged executable, loader, and cookie hashes. The live tests need desktop display access and permission to create user/network namespaces. Recovery files are retained under `/tmp/d2r-live-*` for inspection.
