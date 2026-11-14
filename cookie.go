package main

import "path/filepath"

func cookiePath(prefix string) string {
	return filepath.Join(prefix, "pfx", "drive_c", "users", "steamuser", "AppData", "Local", "Blizzard Entertainment", "ClientSdk", "cookie.bin")
}
