//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOfflineClockKeepsWeekdayAndTimeNearCookieRefresh(t *testing.T) {
	zone, err := time.LoadLocation("Australia/Adelaide")
	if err != nil {
		t.Fatal(err)
	}
	prefix := t.TempDir()
	cookie := cookiePath(prefix)
	if err := os.MkdirAll(filepath.Dir(cookie), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cookie, []byte("test cookie"), 0600); err != nil {
		t.Fatal(err)
	}
	issued := time.Date(2026, 10, 20, 19, 46, 0, 0, zone)
	if err := os.Chtimes(cookie, issued, issued); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 14, 20, 2, 30, 0, zone)
	offset, anchor, err := offlineClockOffset(prefix, now)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 24, 20, 2, 30, 0, zone)
	if !anchor.Equal(want) || offset != int64(want.Sub(now).Seconds()) {
		t.Fatalf("got anchor %s, offset %d; want %s", anchor, offset, want)
	}
	ah, am, as := anchor.Clock()
	nh, nm, ns := now.Clock()
	if anchor.Weekday() != now.Weekday() || ah != nh || am != nm || as != ns {
		t.Fatalf("anchor lost current weekday or time: %s", anchor)
	}
}

func TestOfflineClockRejectsMissingCookie(t *testing.T) {
	if _, _, err := offlineClockOffset(t.TempDir(), time.Now()); err == nil {
		t.Fatal("missing signed cookie should require Battle.net login")
	}
}
