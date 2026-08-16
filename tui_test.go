package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	m, err := newModel(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
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

	m, err := newModel(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
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

	m, err := newModel(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
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

func TestNewModelReportsScanError(t *testing.T) {
	// A path that exists but is not a directory cannot be scanned; the
	// error must surface instead of showing an empty list.
	tmpFile, err := os.CreateTemp("", "tui-notdir")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	_ = tmpFile.Close()

	if _, err := newModel(tmpFile.Name()); err == nil {
		t.Fatal("expected newModel to report the scan error")
	}
}

// tuiModelWithDirs builds a model over n empty subdirectories.
func tuiModelWithDirs(t *testing.T, n int) model {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "tui-model")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	for i := 0; i < n; i++ {
		if err := os.Mkdir(filepath.Join(tmpDir, fmt.Sprintf("repo%02d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m, err := newModel(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestRepairRequiresConfirmation(t *testing.T) {
	m := tuiModelWithDirs(t, 1)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	um := updated.(model)
	if um.pending == nil {
		t.Fatal("expected repair to await confirmation")
	}
	if !strings.Contains(um.pending.prompt, "repair") {
		t.Fatalf("expected a repair prompt, got %q", um.pending.prompt)
	}
	if um.items[0].busy {
		t.Fatal("repair must not start before confirmation")
	}

	// Any key other than y/n is ignored while a confirmation is pending.
	updated, _ = um.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if updated.(model).pending == nil {
		t.Fatal("expected an unrelated key to leave the confirmation pending")
	}

	updated, _ = um.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	cancelled := updated.(model)
	if cancelled.pending != nil {
		t.Fatal("expected n to cancel the confirmation")
	}
	if cancelled.items[0].busy {
		t.Fatal("cancelled repair must not mark the item busy")
	}
}

func TestBusyItemIgnoresSecondAction(t *testing.T) {
	m := tuiModelWithDirs(t, 1)
	m.items[0].busy = true

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	um := updated.(model)
	if !strings.Contains(um.message, "action in progress") {
		t.Fatalf("expected a busy message, got %q", um.message)
	}
	if um.pending != nil {
		t.Fatal("a busy item must not open a confirmation")
	}
}

func TestCommitInputCancels(t *testing.T) {
	m := tuiModelWithDirs(t, 1)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	um := updated.(model)
	if !um.commitInput.Focused() {
		t.Fatal("expected c to focus the commit message input")
	}
	if um.commitInput.Value() != defaultCommitMessage {
		t.Fatalf("expected the default message pre-filled, got %q", um.commitInput.Value())
	}
	if um.items[0].busy {
		t.Fatal("commit must not start before the message is confirmed")
	}

	updated, _ = um.Update(tea.KeyMsg{Type: tea.KeyEsc})
	cancelled := updated.(model)
	if cancelled.commitInput.Focused() {
		t.Fatal("expected esc to dismiss the commit input")
	}
	if cancelled.items[0].busy {
		t.Fatal("cancelled commit must not mark the item busy")
	}
}

func TestCursorStaysWithinWindow(t *testing.T) {
	m := tuiModelWithDirs(t, 40)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: chromeHeight + 5})
	um := updated.(model)

	for i := 0; i < 39; i++ {
		next, _ := um.Update(tea.KeyMsg{Type: tea.KeyDown})
		um = next.(model)
		if um.cursor < um.offset || um.cursor >= um.offset+um.listHeight() {
			t.Fatalf("cursor %d outside window [%d,%d)", um.cursor, um.offset, um.offset+um.listHeight())
		}
	}
	if um.cursor != 39 {
		t.Fatalf("expected cursor at the last item, got %d", um.cursor)
	}

	for i := 0; i < 39; i++ {
		next, _ := um.Update(tea.KeyMsg{Type: tea.KeyUp})
		um = next.(model)
		if um.cursor < um.offset || um.cursor >= um.offset+um.listHeight() {
			t.Fatalf("cursor %d outside window [%d,%d)", um.cursor, um.offset, um.offset+um.listHeight())
		}
	}
}

func TestFailureMessagePersists(t *testing.T) {
	m := tuiModelWithDirs(t, 1)
	path := m.items[0].path

	updated, _ := m.Update(actionDoneMsg{path: path, message: "push failed: boom", failed: true})
	um := updated.(model)
	if !um.messageSticky {
		t.Fatal("expected a failure message to be sticky")
	}
	// The auto-clear tick must not wipe a failure the user has not seen.
	updated, _ = um.Update(clearMsg{})
	if updated.(model).message == "" {
		t.Fatal("expected the failure message to survive the clear tick")
	}
	// The next keypress dismisses it.
	updated, _ = updated.(model).Update(tea.KeyMsg{Type: tea.KeyDown})
	if updated.(model).message != "" {
		t.Fatal("expected the next keypress to dismiss the failure message")
	}
}

func TestActionDoneClearsBusy(t *testing.T) {
	m := tuiModelWithDirs(t, 1)
	m.items[0].busy = true
	path := m.items[0].path

	updated, _ := m.Update(actionDoneMsg{path: path, message: "push done"})
	um := updated.(model)
	if um.items[0].busy {
		t.Fatal("expected the item to be free after the action completed")
	}
	if um.items[0].lastResult != "push done" {
		t.Fatalf("expected the result recorded, got %q", um.items[0].lastResult)
	}
}

func TestViewWindowsLongLists(t *testing.T) {
	m := tuiModelWithDirs(t, 30)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: chromeHeight + 5})
	um := updated.(model)

	view := um.View()
	if !strings.Contains(view, "repo00") {
		t.Fatalf("expected the first repo on screen:\n%s", view)
	}
	if strings.Contains(view, "repo29") {
		t.Fatalf("expected the last repo to be scrolled off:\n%s", view)
	}
	if !strings.Contains(view, "more") {
		t.Fatalf("expected a scroll indicator:\n%s", view)
	}
	if !strings.Contains(view, "[1/30]") {
		t.Fatalf("expected a position indicator:\n%s", view)
	}

	// Every rendered line must fit the window height.
	if lines := strings.Count(view, "\n") + 1; lines > chromeHeight+5 {
		t.Fatalf("view is %d lines, taller than the %d-line window:\n%s", lines, chromeHeight+5, view)
	}
}

func TestViewFitsWindowHeight(t *testing.T) {
	const height = 14

	// The message block and the scroll indicators all consume rows the list
	// cannot also use; the rendered view must fit the window regardless.
	states := []struct {
		name  string
		setup func(m model) model
	}{
		{"no message", func(m model) model { return m }},
		{"short message", func(m model) model { m.message = "push done"; return m }},
		{"multi-line failure", func(m model) model {
			m.message = "push repo03 failed: exit status 1\nfatal: unable to access remote\nhint: check credentials"
			return m
		}},
		{"pending confirmation", func(m model) model {
			m.pending = &pendingAction{prompt: "repair repo03? this moves .git aside (y/n)"}
			return m
		}},
	}

	for _, state := range states {
		for _, scrollDown := range []int{0, 15, 29} {
			m := tuiModelWithDirs(t, 30)
			updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: height})
			um := updated.(model)
			um = state.setup(um)
			for i := 0; i < scrollDown; i++ {
				next, _ := um.Update(tea.KeyMsg{Type: tea.KeyDown})
				um = next.(model)
			}
			view := um.View()
			if lines := strings.Count(view, "\n") + 1; lines > height {
				t.Errorf("%s at row %d: view is %d lines, window is %d:\n%s",
					state.name, scrollDown, lines, height, view)
			}
		}
	}
}

func TestIconCombinesBadges(t *testing.T) {
	item := tuiItem{status: &RepoStatus{
		Type:  Git,
		Dirty: true,
		Ahead: Count{N: 2, Known: true},
	}}
	if got := item.icon(); got != "📝⬆" {
		t.Fatalf("expected dirty and ahead badges, got %q", got)
	}
}

func TestLoadStatusErrorIsNotCorrupted(t *testing.T) {
	// A garbage .jj directory makes getRepoStatus fail without any git
	// damage: the item must surface as an error, not as corrupted.
	tmpDir, err := os.MkdirTemp("", "tui-error")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	jjDir := filepath.Join(tmpDir, ".jj")
	if err := os.Mkdir(jjDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jjDir, "garbage"), []byte("not a repo"), 0o644); err != nil {
		t.Fatal(err)
	}

	msg := loadStatusCmd(0, tmpDir)()
	sm, ok := msg.(statusMsg)
	if !ok {
		t.Fatalf("expected statusMsg, got %T", msg)
	}
	if sm.status == nil {
		t.Fatal("expected a status even when the scan errors")
	}
	if sm.status.Corrupted {
		t.Fatal("a scan error must not mark the repo corrupted")
	}
	if sm.status.Error == "" {
		t.Fatal("expected the error message to be recorded")
	}

	item := tuiItem{path: tmpDir, status: sm.status}
	if got := item.icon(); got != "⚠️" {
		t.Fatalf("expected warning icon for errored repo, got %q", got)
	}
	if got := item.statusText(); !strings.HasPrefix(got, "error: ") {
		t.Fatalf("expected \"error: ...\" status text, got %q", got)
	}
}
