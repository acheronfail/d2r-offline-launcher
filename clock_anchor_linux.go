//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// offlineClockOffset uses the signed cookie's timestamp to keep the game near
// the start of its window, with today's weekday and local wall-clock time.
func offlineClockOffset(prefix string, now time.Time) (int64, time.Time, error) {
	info, err := os.Stat(cookiePath(prefix))
	if errors.Is(err, os.ErrNotExist) {
		return 0, time.Time{}, errors.New("Battle.net login cookie is missing; sign in once through Battle.net before offline play")
	}
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("Read Battle.net login cookie: %w", err)
	}
	if info.IsDir() || info.Size() == 0 {
		return 0, time.Time{}, errors.New("Battle.net login cookie is empty or invalid; sign in through Battle.net")
	}
	issued := info.ModTime().In(now.Location())
	if issued.After(now.Add(time.Hour)) || now.Sub(issued) > 10*365*24*time.Hour {
		return 0, time.Time{}, errors.New("Battle.net login cookie timestamp is outside the supported range; refresh it through Battle.net")
	}
	days := (int(now.Weekday()) - int(issued.Weekday()) + 7) % 7
	anchor := time.Date(issued.Year(), issued.Month(), issued.Day()+days,
		now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), now.Location())
	if anchor.Before(issued.Add(time.Hour)) {
		anchor = anchor.AddDate(0, 0, 7)
	}
	if anchor.After(now) {
		// A freshly refreshed cookie is already valid at the real time.
		anchor = now
	}
	return int64(anchor.Sub(now).Seconds()), anchor, nil
}
