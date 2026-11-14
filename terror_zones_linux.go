//go:build linux

package main

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const terrorZoneRelativePath = "Data/hd/global/excel/desecratedzones.json"
const terrorZoneBackupName = "terror-zones.original.json"
const terrorZoneRecordName = "terror-zones-state.json"
const terrorZoneHorizon = 7 * 24 * time.Hour
const terrorTimeLayout = "2006-01-02 15:04:05"

type terrorZoneRecord struct {
	Path          string `json:"path"`
	OriginalSHA   string `json:"original_sha256"`
	GeneratedSHA  string `json:"generated_sha256"`
	PreviousSHA   string `json:"previous_sha256,omitempty"`
	GeneratedTill string `json:"generated_till_utc"`
}

type terrorZoneSource struct {
	root   map[string]json.RawMessage
	config map[string]json.RawMessage
	start  time.Time
	seed   uint64
	period time.Duration
	zones  []json.RawMessage
}

func terrorZonePath(game string) string { return filepath.Join(game, terrorZoneRelativePath) }

func usesExtractedTerrorZones(m manifest) bool {
	args := gameArgsFromOptions(m.LaunchOptions)
	direct, txt := false, false
	for _, arg := range args {
		if strings.EqualFold(arg, "-direct") {
			direct = true
		}
		if strings.EqualFold(arg, "-txt") {
			txt = true
		}
	}
	return direct && txt
}

func parseTerrorZoneSource(data []byte) (terrorZoneSource, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return terrorZoneSource{}, fmt.Errorf("Parse Terror Zone JSON: %w", err)
	}
	var configs []json.RawMessage
	if err := json.Unmarshal(root["desecrated_zones"], &configs); err != nil || len(configs) != 1 {
		return terrorZoneSource{}, errors.New("Terror Zone JSON needs the supported single 3.x rotation config")
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(configs[0], &config); err != nil {
		return terrorZoneSource{}, errors.New("Terror Zone rotation config is invalid")
	}
	var startText, endText string
	var seed uint64
	var durationMinutes, breakMinutes int
	var zones []json.RawMessage
	for _, field := range []struct {
		name string
		out  any
	}{{"start_time_utc", &startText}, {"end_time_utc", &endText}, {"seed", &seed},
		{"zone_duration_minutes", &durationMinutes}, {"break_duration_minutes", &breakMinutes}, {"zones", &zones}} {
		if err := json.Unmarshal(config[field.name], field.out); err != nil {
			return terrorZoneSource{}, fmt.Errorf("Terror Zone config lacks %s: %w", field.name, err)
		}
	}
	start, err := time.ParseInLocation(terrorTimeLayout, startText, time.UTC)
	if err != nil || endText != "" || durationMinutes != 30 || breakMinutes != 0 || len(zones) < 2 || len(zones) > 100 {
		return terrorZoneSource{}, errors.New("Terror Zone schedule format changed; no game file was edited")
	}
	for _, zone := range zones {
		var item struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(zone, &item) != nil || item.ID == "" {
			return terrorZoneSource{}, errors.New("Terror Zone list contains an invalid zone")
		}
	}
	return terrorZoneSource{root: root, config: config, start: start, seed: seed,
		period: time.Duration(durationMinutes) * time.Minute, zones: zones}, nil
}

func terrorRoll(at time.Time, seed uint64) float64 {
	var bytes [8]byte
	binary.LittleEndian.PutUint64(bytes[:], uint64(at.Unix())^seed)
	digest := sha1.Sum(bytes[:])
	return float64(binary.LittleEndian.Uint32(digest[:4])) / 4294967296.0
}

func terrorZonePicks(source terrorZoneSource, first, last int64) ([]int, error) {
	if first < 0 || last < first || last > 1000000 {
		return nil, errors.New("Terror Zone schedule is outside the supported time range")
	}
	count := len(source.zones)
	weights := make([]float64, count)
	for i := range weights {
		weights[i] = 1 / float64(count-1)
	}
	previous := int(math.Floor(terrorRoll(source.start.Add(-source.period), source.seed) * float64(count)))
	weights[previous] = 0
	picks := make([]int, 0, last-first+1)
	for slot := int64(0); slot <= last; slot++ {
		at := source.start.Add(time.Duration(slot) * source.period)
		roll := terrorRoll(at, source.seed)
		cumulative, pick, share := 0.0, count-1, 0.0
		for i, weight := range weights {
			cumulative += weight
			if roll < cumulative {
				pick, share = i, weight/float64(count-1)
				break
			}
		}
		for i := range weights {
			weights[i] += share
		}
		weights[pick] = 0
		if slot >= first {
			picks = append(picks, pick)
		}
	}
	return picks, nil
}

func generateTerrorZoneSchedule(original []byte, now time.Time, offsetSeconds int64) ([]byte, time.Time, error) {
	source, err := parseTerrorZoneSource(original)
	if err != nil {
		return nil, time.Time{}, err
	}
	first := int64(now.UTC().Sub(source.start) / source.period)
	last := int64(now.Add(terrorZoneHorizon).UTC().Sub(source.start) / source.period)
	picks, err := terrorZonePicks(source, first, last)
	if err != nil {
		return nil, time.Time{}, err
	}
	shift := time.Duration(offsetSeconds) * time.Second
	configs := make([]json.RawMessage, 0, len(picks))
	for i, pick := range picks {
		realStart := source.start.Add(time.Duration(first+int64(i)) * source.period)
		fakeStart := realStart.Add(shift)
		entry := make(map[string]json.RawMessage, len(source.config))
		for key, value := range source.config {
			entry[key] = value
		}
		for key, value := range map[string]any{
			"name":                  fmt.Sprintf("d2r_offline_slot_%d", i),
			"start_time_utc":        fakeStart.Format(terrorTimeLayout),
			"end_time_utc":          fakeStart.Add(source.period).Format(terrorTimeLayout),
			"zone_duration_minutes": int(source.period / time.Minute),
			"zones":                 []json.RawMessage{source.zones[pick]},
		} {
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, time.Time{}, err
			}
			entry[key] = encoded
		}
		encoded, err := json.Marshal(entry)
		if err != nil {
			return nil, time.Time{}, err
		}
		configs = append(configs, encoded)
	}
	encodedConfigs, err := json.Marshal(configs)
	if err != nil {
		return nil, time.Time{}, err
	}
	root := make(map[string]json.RawMessage, len(source.root))
	for key, value := range source.root {
		root[key] = value
	}
	root["desecrated_zones"] = encodedConfigs
	generated, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, time.Time{}, err
	}
	end := source.start.Add(time.Duration(last+1) * source.period)
	return append(generated, '\n'), end, nil
}

func terrorSHA(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func readTerrorZoneRecord(state string) (terrorZoneRecord, error) {
	data, err := os.ReadFile(filepath.Join(state, terrorZoneRecordName))
	if err != nil {
		return terrorZoneRecord{}, err
	}
	var record terrorZoneRecord
	if err := json.Unmarshal(data, &record); err != nil || record.Path == "" || record.OriginalSHA == "" {
		return terrorZoneRecord{}, errors.New("Terror Zone restoration record is invalid")
	}
	return record, nil
}

func atomicGameFile(path string, data []byte, mode os.FileMode) error {
	out, err := os.CreateTemp(filepath.Dir(path), ".d2r-offline-tz-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	if err = out.Chmod(mode.Perm()); err == nil {
		_, err = out.Write(data)
	}
	if err == nil {
		err = out.Sync()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(out.Name(), path)
}

func prepareTerrorZones(state string, m manifest) error {
	if !usesExtractedTerrorZones(m) {
		return nil
	}
	path := terrorZonePath(m.Game)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if _, err := readTerrorZoneRecord(state); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	backup := filepath.Join(state, terrorZoneBackupName)
	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if existing, err := os.ReadFile(backup); err == nil {
		if terrorSHA(existing) != terrorSHA(original) {
			return errors.New("Terror Zone backup exists but game JSON changed; inspect it before continuing")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := parseTerrorZoneSource(original); err != nil {
		return err
	}
	return atomicGameFile(backup, original, 0600)
}

func alignTerrorZones(state string, m manifest, now time.Time, offsetSeconds int64) error {
	if !usesExtractedTerrorZones(m) {
		return nil
	}
	path := terrorZonePath(m.Game)
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	backupPath := filepath.Join(state, terrorZoneBackupName)
	backup, err := os.ReadFile(backupPath)
	if errors.Is(err, os.ErrNotExist) {
		if err := prepareTerrorZones(state, m); err != nil {
			return err
		}
		backup, err = os.ReadFile(backupPath)
	}
	if err != nil {
		return err
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	record, err := readTerrorZoneRecord(state)
	if errors.Is(err, os.ErrNotExist) {
		record = terrorZoneRecord{Path: path, OriginalSHA: terrorSHA(backup)}
	} else if err != nil {
		return err
	}
	if record.Path != path || record.OriginalSHA != terrorSHA(backup) {
		return errors.New("Terror Zone backup does not match its restoration record")
	}
	currentSHA := terrorSHA(current)
	if currentSHA != record.OriginalSHA && currentSHA != record.GeneratedSHA && currentSHA != record.PreviousSHA {
		return errors.New("Terror Zone JSON changed outside this app; preserve it and reinstall after resolving the change")
	}
	generated, end, err := generateTerrorZoneSchedule(backup, now, offsetSeconds)
	if err != nil {
		return err
	}
	if currentSHA != record.OriginalSHA {
		record.PreviousSHA = currentSHA
	} else {
		record.PreviousSHA = ""
	}
	record.GeneratedSHA = terrorSHA(generated)
	record.GeneratedTill = end.UTC().Format(time.RFC3339)
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicGameFile(filepath.Join(state, terrorZoneRecordName), encoded, 0600); err != nil {
		return err
	}
	if currentSHA == record.GeneratedSHA {
		return nil
	}
	return atomicGameFile(path, generated, info.Mode())
}

func restoreTerrorZones(state string, m manifest) error {
	record, err := readTerrorZoneRecord(state)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if gameRunningInPrefix(m.Prefix) {
		return errors.New("Close D2R in this Proton prefix before restoring Terror Zone data")
	}
	if record.Path != terrorZonePath(m.Game) {
		return errors.New("Terror Zone restoration path differs from the installed game")
	}
	backup, err := os.ReadFile(filepath.Join(state, terrorZoneBackupName))
	if err != nil || terrorSHA(backup) != record.OriginalSHA {
		return errors.New("Terror Zone backup is missing or damaged")
	}
	current, err := os.ReadFile(record.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if terrorSHA(current) != record.GeneratedSHA && terrorSHA(current) != record.PreviousSHA {
		return nil // Battle.net or another editor changed it; preserve their newer file.
	}
	info, err := os.Stat(record.Path)
	if err != nil {
		return err
	}
	return atomicGameFile(record.Path, backup, info.Mode())
}
