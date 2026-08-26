package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	modeTUI  = "tui"
	modeList = "list"
)

// config contains user defaults loaded from the XDG configuration directory.
// Command-line mode flags take precedence over it.
type config struct {
	DefaultMode string `toml:"default_mode"`
}

func defaultConfig() config {
	return config{DefaultMode: modeTUI}
}

func configPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir != "" {
		return filepath.Join(dir, "gitsync", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory for configuration: %w", err)
	}
	return filepath.Join(home, ".config", "gitsync", "config.toml"), nil
}

// loadConfig reads a strict TOML schema. A missing file means defaults; a
// misspelled key is an error rather than a setting that silently does nothing.
func loadConfig(path string) (config, error) {
	cfg := defaultConfig()
	metadata, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, 0, len(undecoded))
		for _, key := range undecoded {
			keys = append(keys, key.String())
		}
		return config{}, fmt.Errorf("read config %s: unknown key%s %s", path, pluralSuffix(len(keys)), strings.Join(keys, ", "))
	}
	if cfg.DefaultMode != modeTUI && cfg.DefaultMode != modeList {
		return config{}, fmt.Errorf("read config %s: default_mode must be %q or %q, got %q", path, modeTUI, modeList, cfg.DefaultMode)
	}
	return cfg, nil
}

func pluralSuffix(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// shouldRunTUI resolves explicit flags, the configured default, and terminal
// capability in that order. Bulk operations always remain in the line-based
// interface because they own stdin for confirmation and stream their results.
func shouldRunTUI(opts options, cfg config, stdinTTY, stdoutTTY bool) bool {
	if opts.interactive {
		return true
	}
	if opts.list {
		return false
	}
	if _, bulk := opts.bulkOp(); bulk {
		return false
	}
	// Report-specific flags retain their established line-oriented meaning;
	// they are also an explicit request not to use the automatic TUI.
	if opts.filter != "" || opts.sort != "" || opts.showAll || opts.recursive {
		return false
	}
	return cfg.DefaultMode == modeTUI && stdinTTY && stdoutTTY
}
