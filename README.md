# D2R Offline Launcher

Single-file Linux/Proton GUI for playing Battle.net Diablo II: Resurrected offline with its cached license cookie.

## Steam Deck

1. Transfer `release/D2R-Offline-Launcher-Linux`, mark it executable in Desktop Mode, and open it.
2. Check the detected D2R folder and Battle.net Proton prefix, then tap **Install**.
3. Point the existing D2R Steam shortcut at the installed `D2R-Offline-Launcher` beside the game folder as a native Linux app, and clear its old launch options. Keep the separate Battle.net shortcut for online play and updates.
4. Open the D2R shortcut and tap **Launch Offline**. Tap **Uninstall** before updating through Battle.net, then install again when ready.

Install places the GUI beside D2R and stores offline support files under `~/.local/share/d2r-offline-launcher`. Uninstall restores game data and keeps the GUI and saved paths ready for another install. Offline launches use a private network namespace and a process-only clock shift near the start of the cookie's window. The cached cookie must include your purchased-content entitlements.

With extracted game data and `-direct -txt`, offline launches align Terror Zones with real time. Uninstall restores the saved `desecratedzones.json`; newer edits are preserved. Launch again to extend the seven-day schedule. The rotation follows [Arcane Tracker's findings](https://github.com/hyperremix/d2r-arcane-tracker/blob/main/docs/TERROR_ZONE_FINDINGS_2026.md).

## Build

Run `just package` to test and build the single executable in `release/`. `just test`, `just build`, and `just clean` are also available. Building requires Go, GCC, and Fyne's X11/OpenGL development libraries. The binary requires glibc 2.38 or newer.
