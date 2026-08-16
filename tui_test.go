package main

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTUIModelInit(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tui-init")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	if err := os.Mkdir(filepath.Join(tmpDir, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := newModel(tmpDir)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected init command")
	}
}

func TestTUIModelUpdateNavigation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tui-nav")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create two subdirectories.
	if err := os.Mkdir(filepath.Join(tmpDir, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(tmpDir, "b"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := newModel(tmpDir)
	if len(m.items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(m.items))
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	um := updated.(model)
	if um.cursor != 1 {
		t.Fatalf("expected cursor 1 after down, got %d", um.cursor)
	}

	updated, _ = um.Update(tea.KeyMsg{Type: tea.KeyUp})
	um = updated.(model)
	if um.cursor != 0 {
		t.Fatalf("expected cursor 0 after up, got %d", um.cursor)
	}
}

func TestTUIStatusMessage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tui-status")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	if err := os.Mkdir(filepath.Join(tmpDir, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := newModel(tmpDir)
	updated, _ := m.Update(statusMsg{index: 0, status: &RepoStatus{Path: tmpDir, Type: Bare}})
	um := updated.(model)
	if um.items[0].status == nil {
		t.Fatal("expected status to be set")
	}
}

func TestTUIInvalidDirectory(t *testing.T) {
	err := runTUI("/nonexistent/path/for/gitsync")
	if err == nil {
		t.Fatal("expected error for invalid directory")
	}
}
