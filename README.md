# D2R Offline Launcher

One-file Linux/Proton GUI for launching Battle.net Diablo II: Resurrected offline with its existing signed login cookie. Windows is out of scope.

## Steam Deck

1. Transfer `release/D2R-Offline-Launcher-Linux`, mark it executable in Desktop Mode, and open it.
2. Check the detected D2R folder and Battle.net Proton prefix, then tap **Install**.
3. Point the existing D2R Steam shortcut at the installed `D2R-Offline-Launcher` beside the game folder as a native Linux app, and clear its old launch options. Keep the separate Battle.net shortcut for online play and updates.
4. Open the D2R shortcut and tap **Launch Offline**. Tap **Uninstall** before updating through Battle.net, then install again when ready.

Install saves the GUI launcher beside D2R, with offline support files and settings under `~/.local/share/d2r-offline-launcher`. Uninstall removes the offline library and restores game data while keeping the GUI and saved paths available. The app does not edit Steam shortcuts or close Steam. Offline launches use a private network namespace and a process-only clock shift near the start of the cookie's window. The system clock, cookie, prefix, registry, and Battle.net shortcuts stay untouched. A valid cookie with purchased-content entitlements is still required.

With extracted game data and `-direct -txt`, offline launches align Terror Zones with real time. Uninstall restores the saved `desecratedzones.json`; if another program changed it, the newer file is preserved. Launch again to extend the seven-day schedule.

## Build

Run `just package` to test and build the single executable in `release/`. `just test`, `just build`, and `just clean` are also available. Building requires Go, GCC, and Fyne's X11/OpenGL development libraries. The binary requires glibc 2.38 or newer.

## Evidence and limits

On this PC, GUI launch, Warlock gameplay, Uninstall restoration, and a real-time Terror Zone rollover worked. A missing cookie disabled the Warlock expansion; the clock shift must last for the entire game session. The rotation logic follows [Arcane Tracker's findings](https://github.com/hyperremix/d2r-arcane-tracker/blob/main/docs/TERROR_ZONE_FINDINGS_2026.md). Steam Deck Desktop and Gaming Mode still need testing.
