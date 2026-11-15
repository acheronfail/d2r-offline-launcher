package main

import (
	"os"
	"path/filepath"
	"testing"
)

func allowFixtureBuild(t *testing.T) {
	previous := verifySelectedBuild
	verifySelectedBuild = func(string) error { return nil }
	t.Cleanup(func() { verifySelectedBuild = previous })
}

func TestProtonMatchesPrefixVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	common := filepath.Join(home, ".steam", "steam", "steamapps", "common")
	for _, candidate := range []struct{ name, version string }{
		{"Proton 10.0", "1785139158 proton-10.0-4b"},
		{"Proton - Experimental", "1793301796 experimental-11.0-20261027b-x86_64"},
	} {
		folder := filepath.Join(common, candidate.name)
		if e := os.MkdirAll(folder, 0700); e != nil {
			t.Fatal(e)
		}
		for name, value := range map[string]string{"proton": "", "version": candidate.version} {
			if e := os.WriteFile(filepath.Join(folder, name), []byte(value), 0700); e != nil {
				t.Fatal(e)
			}
		}
	}
	runtime := filepath.Join(common, "SteamLinuxRuntime_sniper", "run")
	if e := os.MkdirAll(filepath.Dir(runtime), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(runtime, nil, 0700); e != nil {
		t.Fatal(e)
	}
	prefix := filepath.Join(home, "prefix")
	if e := os.MkdirAll(prefix, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(prefix, "version"), []byte("10.1000-105"), 0600); e != nil {
		t.Fatal(e)
	}
	_, proton, _, e := protonPaths(prefix)
	if e != nil || filepath.Base(filepath.Dir(proton)) != "Proton 10.0" {
		t.Fatalf("Selected %q: %v", proton, e)
	}
}

func TestProtonUsesSteamConfiguredTool(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	steam := filepath.Join(home, ".steam", "steam")
	common := filepath.Join(steam, "steamapps", "common")
	for _, name := range []string{"Proton 10.0", "Proton 11.0"} {
		folder := filepath.Join(common, name)
		if err := os.MkdirAll(folder, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, "proton"), nil, 0700); err != nil {
			t.Fatal(err)
		}
	}
	runtime := filepath.Join(common, "SteamLinuxRuntime_sniper", "run")
	if err := os.MkdirAll(filepath.Dir(runtime), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtime, nil, 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(steam, "config", "config.vdf")
	if err := os.MkdirAll(filepath.Dir(config), 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`"InstallConfigStore" { "Software" { "Valve" { "Steam" { "CompatToolMapping" { "12345" { "name" "proton_10" } } } } } }`)
	if err := os.WriteFile(config, data, 0600); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(steam, "steamapps", "compatdata", "12345")
	if err := os.MkdirAll(prefix, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefix, "version"), []byte("11.0-100"), 0600); err != nil {
		t.Fatal(err)
	}
	_, proton, _, err := protonPaths(prefix)
	if err != nil || filepath.Base(filepath.Dir(proton)) != "Proton 10.0" {
		t.Fatalf("Selected %q: %v", proton, err)
	}
}
