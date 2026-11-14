//go:build linux

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTerrorZoneAlignmentRestoresOriginalFile(t *testing.T) {
	game, state := t.TempDir(), t.TempDir()
	path := terrorZonePath(game)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	original, err := json.Marshal(map[string]any{
		"type": "DesecratedZoneConfig",
		"desecrated_zones": []any{map[string]any{
			"type": "DesecratedConfigData", "name": "retail", "start_time_utc": "2025-12-05 00:00:00",
			"end_time_utc": "", "seed": uint64(16665365343970128666),
			"zone_duration_minutes": 30, "break_duration_minutes": 0,
			"zones": []any{
				map[string]any{"id": "a", "name": "zone_a"},
				map[string]any{"id": "b", "name": "zone_b"},
				map[string]any{"id": "c", "name": "zone_c"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	m := manifest{Game: game, Version: 4, LaunchOptions: "%command% -direct -txt"}
	if err := prepareTerrorZones(state, m); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	if err := alignTerrorZones(state, m, now, -21*86400); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		DesecratedZones []struct {
			StartTimeUTC string `json:"start_time_utc"`
			EndTimeUTC   string `json:"end_time_utc"`
			Seed         uint64 `json:"seed"`
			Zones        []struct {
				ID string `json:"id"`
			} `json:"zones"`
		} `json:"desecrated_zones"`
	}
	if err := json.Unmarshal(generated, &root); err != nil {
		t.Fatal(err)
	}
	if len(root.DesecratedZones) != 337 {
		t.Fatalf("got %d generated slots, want 337", len(root.DesecratedZones))
	}
	first := root.DesecratedZones[0]
	if first.StartTimeUTC != "2026-10-24 09:00:00" || first.EndTimeUTC != "2026-10-24 09:30:00" ||
		first.Seed != 16665365343970128666 || len(first.Zones) != 1 {
		t.Fatalf("generated slot changed date, seed, or zone count: %+v", first)
	}
	if err := restoreTerrorZones(state, m); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(restored, original) {
		t.Fatal("Uninstall did not restore the exact original Terror Zone JSON")
	}
	if err := alignTerrorZones(state, m, now, -21*86400); err != nil {
		t.Fatal(err)
	}
	record, err := readTerrorZoneRecord(state)
	if err != nil {
		t.Fatal(err)
	}
	record.PreviousSHA = record.GeneratedSHA
	record.GeneratedSHA = strings.Repeat("0", 64) // Record was written; replacement file was not.
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, terrorZoneRecordName), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := restoreTerrorZones(state, m); err != nil {
		t.Fatal(err)
	}
	restored, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(restored, original) {
		t.Fatal("Interrupted refresh did not restore the original Terror Zone JSON")
	}
}

func TestRetailTerrorZoneRollVector(t *testing.T) {
	game := os.Getenv("D2R_OFFLINE_INSPECT_GAME")
	if game == "" {
		t.Skip("set D2R_OFFLINE_INSPECT_GAME to check the current retail JSON")
	}
	data, err := os.ReadFile(terrorZonePath(game))
	if err != nil {
		t.Fatal(err)
	}
	source, err := parseTerrorZoneSource(data)
	if err != nil {
		t.Fatal(err)
	}
	if source.seed != 16665365343970128666 || source.start.Format(terrorTimeLayout) != "2025-12-05 00:00:00" {
		t.Skip("retail rotation seed or start changed")
	}
	at := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	first := int64(at.Sub(source.start) / source.period)
	picks, err := terrorZonePicks(source, first, first+3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Act3-GreatMarsh", "Act3-FlayerJungle", "Act1-Monastery", "Act2-TalRashas"}
	for i, pick := range picks {
		var zone struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(source.zones[pick], &zone); err != nil {
			t.Fatal(err)
		}
		if !strings.EqualFold(zone.ID, want[i]) {
			t.Fatalf("slot %d: got %s, want %s", i, zone.ID, want[i])
		}
	}
	generated, end, err := generateTerrorZoneSchedule(data, at, -21*86400)
	if err != nil {
		t.Fatal(err)
	}
	var mapped struct {
		Configs []json.RawMessage `json:"desecrated_zones"`
	}
	if err := json.Unmarshal(generated, &mapped); err != nil || len(mapped.Configs) != 337 {
		t.Fatalf("generated retail JSON has %d slots: %v", len(mapped.Configs), err)
	}
	t.Logf("Generated %d aligned slots (%d bytes) through %s", len(mapped.Configs), len(generated), end.UTC().Format(time.RFC3339))
}
