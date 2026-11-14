//go:build linux

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCompatMappingRemovesAndRestoresOnlyD2R(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "config.vdf")
	before := []byte("\"InstallConfigStore\"\n{\n\t\"Software\"\n\t{\n\t\t\"Valve\"\n\t\t{\n\t\t\t\"Steam\"\n\t\t\t{\n\t\t\t\t\"CompatToolMapping\"\n\t\t\t\t{\n\t\t\t\t\t\"2612526253\"\n\t\t\t\t\t{\n\t\t\t\t\t\t\"name\"\t\"proton_9\"\n\t\t\t\t\t}\n\t\t\t\t\t\"4292372109\"\n\t\t\t\t\t{\n\t\t\t\t\t\t\"name\"\t\"proton_experimental\"\n\t\t\t\t\t}\n\t\t\t\t}\n\t\t\t}\n\t\t}\n\t}\n}\n")
	if err := os.WriteFile(file, before, 0600); err != nil {
		t.Fatal(err)
	}
	block, err := compatBlock(before, "4292372109")
	if err != nil || block == "" {
		t.Fatalf("D2R mapping was not found: %v", err)
	}
	if err := editCompatMapping(file, "4292372109", block, false); err != nil {
		t.Fatal(err)
	}
	without, err := os.ReadFile(file)
	if err != nil || bytes.Contains(without, []byte("4292372109")) || !bytes.Contains(without, []byte("2612526253")) {
		t.Fatal("D2R compatibility mapping was not removed independently")
	}
	if err := editCompatMapping(file, "4292372109", block, true); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("D2R compatibility mapping was not restored exactly")
	}
}

func TestInspectLiveCompatReadOnly(t *testing.T) {
	file := os.Getenv("D2R_CAMPING_INSPECT_CONFIG")
	if file == "" {
		t.Skip("set D2R_CAMPING_INSPECT_CONFIG for a read-only live config check")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compatBlock(data, "4292372109"); err != nil {
		t.Fatal(err)
	}
	t.Log("Parsed the live Steam compatibility map without changing it")
}
