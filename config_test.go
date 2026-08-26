package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigDefaultsWhenMissing(t *testing.T) {
	cfg, err := loadConfig(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultMode != modeTUI {
		t.Fatalf("default mode = %q, expected %q", cfg.DefaultMode, modeTUI)
	}
}

func TestConfigPathUsesXDGConfigHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "gitsync", "config.toml")
	if path != want {
		t.Fatalf("configPath() = %q, expected %q", path, want)
	}
}

func TestLoadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("default_mode = \"list\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultMode != modeList {
		t.Fatalf("default mode = %q, expected %q", cfg.DefaultMode, modeList)
	}
}

func TestLoadConfigRejectsUnknownKeysAndModes(t *testing.T) {
	for name, contents := range map[string]string{
		"unknown key":  "other_mode = \"list\"\n",
		"unknown mode": "default_mode = \"auto\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadConfig(path); err == nil {
				t.Fatal("expected invalid configuration to fail")
			}
		})
	}
}

func TestShouldRunTUI(t *testing.T) {
	base := options{}
	tests := []struct {
		name                string
		opts                options
		cfg                 config
		stdinTTY, stdoutTTY bool
		want                bool
	}{
		{"terminal default", base, defaultConfig(), true, true, true},
		{"redirected stdin", base, defaultConfig(), false, true, false},
		{"redirected stdout", base, defaultConfig(), true, false, false},
		{"configured list", base, config{DefaultMode: modeList}, true, true, false},
		{"explicit list", options{list: true}, defaultConfig(), true, true, false},
		{"explicit interactive", options{interactive: true}, config{DefaultMode: modeList}, false, false, true},
		{"bulk operation", options{pushAll: true}, defaultConfig(), true, true, false},
		{"filter requests list", options{filter: "dirty"}, defaultConfig(), true, true, false},
		{"recursive requests list", options{recursive: true}, defaultConfig(), true, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRunTUI(tt.opts, tt.cfg, tt.stdinTTY, tt.stdoutTTY); got != tt.want {
				t.Fatalf("shouldRunTUI() = %v, expected %v", got, tt.want)
			}
		})
	}
}

func TestListAndInteractiveAreMutuallyExclusive(t *testing.T) {
	_, err := parseArgs([]string{"--list", "--interactive"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected a mutual-exclusion error, got %v", err)
	}
}
