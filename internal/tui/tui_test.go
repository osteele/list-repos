package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/osteele/gitsync/internal/actions"
	"github.com/osteele/gitsync/internal/vcs"
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
	updated, _ := m.Update(statusMsg{path: m.items[0].path, status: &vcs.RepoStatus{Path: tmpDir, Type: vcs.Dir}})
	um := updated.(model)
	if um.items[0].status == nil {
		t.Fatal("expected status to be set")
	}
}

func TestTUIInvalidDirectory(t *testing.T) {
	err := RunTUI("/nonexistent/path/for/gitsync")
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
	if um.commitInput.Value() != actions.DefaultCommitMessage {
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
	item := tuiItem{status: &vcs.RepoStatus{
		Type:  vcs.Git,
		Dirty: true,
		Ahead: vcs.Count{N: 2, Known: true},
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

	msg := loadStatusCmd(tmpDir)()
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

// forceColorProfile makes lipgloss emit escape sequences even though tests
// don't run on a TTY; without it the styling assertions below would pass
// vacuously against unstyled output.
func forceColorProfile(t *testing.T) {
	t.Helper()
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
}

// reverseVideo matches an SGR sequence that includes parameter 7.
var reverseVideo = regexp.MustCompile("\x1b\\[(?:[0-9]+;)*7(?:;[0-9]+)*m")

// ansiSeq matches any SGR escape, so tests can measure layout on the
// text the terminal actually lays out.
var ansiSeq = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiSeq.ReplaceAllString(s, "") }

func TestSelectedRowUsesReverseVideo(t *testing.T) {
	forceColorProfile(t)

	m := tuiModelWithDirs(t, 3)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	um := updated.(model)

	var selected, unselected []string
	for _, line := range strings.Split(um.View(), "\n") {
		if !strings.Contains(line, "repo0") {
			continue
		}
		if strings.Contains(line, "> ") {
			selected = append(selected, line)
		} else {
			unselected = append(unselected, line)
		}
	}
	if len(selected) != 1 {
		t.Fatalf("expected exactly one selected row, got %d:\n%s", len(selected), um.View())
	}
	if !strings.HasPrefix(selected[0], "\x1b[") {
		t.Fatalf("expected the selected row to carry styling, got %q", selected[0])
	}
	if !reverseVideo.MatchString(selected[0]) {
		t.Fatalf("expected reverse video on the selected row, got %q", selected[0])
	}
	// The marker stays as a non-color cue even where styling is stripped.
	// Interior spacing is column alignment's business, so assert on the
	// marker and the row's identity rather than on exact padding.
	if !strings.Contains(selected[0], "> ") || !strings.Contains(selected[0], "repo00") {
		t.Fatalf("expected the > marker on the selected row, got %q", selected[0])
	}
	if len(unselected) != 2 {
		t.Fatalf("expected two unselected rows, got %d", len(unselected))
	}
	for _, line := range unselected {
		if reverseVideo.MatchString(line) {
			t.Fatalf("unselected row must not be reversed: %q", line)
		}
	}
}

func TestSelectedRowFitsTerminalWidth(t *testing.T) {
	forceColorProfile(t)

	const width = 60
	m := tuiModelWithDirs(t, 3)
	// Give the rows emoji badges and status text: inconsistent cell widths
	// there are what could push a padded bar past the right edge.
	for i := range m.items {
		m.items[i].status = &vcs.RepoStatus{
			Type:  vcs.Git,
			Dirty: true,
			Ahead: vcs.Count{N: 2, Known: true},
		}
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 20})
	um := updated.(model)

	var selectedWidth int
	for _, line := range strings.Split(um.View(), "\n") {
		if !strings.Contains(line, "repo0") {
			continue
		}
		if w := lipgloss.Width(line); w > width {
			t.Errorf("list row is %d cells wide in a %d-cell window: %q", w, width, line)
		}
		if strings.Contains(line, "> ") {
			selectedWidth = lipgloss.Width(line)
		}
	}
	// The selection bar spans the full window; padding lands it exactly.
	if selectedWidth != width {
		t.Errorf("expected the selection bar to span %d cells, got %d", width, selectedWidth)
	}
}

func TestNoHardcodedForegroundColors(t *testing.T) {
	// A bare lipgloss.Color("#...") foreground is picked for one terminal
	// theme and illegible on the other; only AdaptiveColor is allowed.
	hardcoded := regexp.MustCompile(`Foreground\(lipgloss\.Color\("#`)
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if hardcoded.Match(data) {
			t.Errorf("%s contains a hardcoded foreground color", path)
		}
	}
}

// TestRowColumnsAlign guards the readability property that the badge
// column is status-only and fixed-width: names and types must start at the
// same screen column on every row, whatever badges a row carries. Emoji
// cell widths differ (📝 is two cells, ⬆ one), so this is the failure mode
// that recurs whenever a row is built by concatenation instead of columns.
func TestRowColumnsAlign(t *testing.T) {
	forceColorProfile(t)
	m := tuiModelWithDirs(t, 5)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	um := updated.(model)

	// One row per badge shape: none, one wide, one narrow, two combined,
	// and a container (no badge at all).
	um.items[0].status = &vcs.RepoStatus{Type: vcs.Git, Remote: true, Ahead: vcs.Count{Known: true}}
	um.items[1].status = &vcs.RepoStatus{Type: vcs.Git, Remote: true, Dirty: true}
	um.items[2].status = &vcs.RepoStatus{Type: vcs.Jujutsu, Remote: true, Ahead: vcs.Count{N: 2, Known: true}}
	um.items[3].status = &vcs.RepoStatus{Type: vcs.Jujutsu, Remote: true, Dirty: true, Ahead: vcs.Count{N: 1, Known: true}}
	um.items[4].status = &vcs.RepoStatus{Type: vcs.Dir, NestedRepos: 3}

	var nameCols, typeCols []int
	for _, line := range strings.Split(um.View(), "\n") {
		if !strings.Contains(line, "repo0") {
			continue
		}
		plain := stripANSI(line)
		idx := strings.Index(plain, "repo0")
		nameCols = append(nameCols, lipgloss.Width(plain[:idx]))
		// The type column begins after the padded name.
		rest := plain[idx:]
		off := strings.IndexAny(rest, " ")
		trimmed := strings.TrimLeft(rest[off:], " ")
		typeCols = append(typeCols, lipgloss.Width(plain[:len(plain)-len(trimmed)]))
	}
	if len(nameCols) != 5 {
		t.Fatalf("expected 5 rows, got %d:\n%s", len(nameCols), um.View())
	}
	for i := range nameCols {
		if nameCols[i] != nameCols[0] {
			t.Errorf("name column starts at %d on row %d but %d on row 0:\n%s",
				nameCols[i], i, nameCols[0], um.View())
		}
		if typeCols[i] != typeCols[0] {
			t.Errorf("type column starts at %d on row %d but %d on row 0:\n%s",
				typeCols[i], i, typeCols[0], um.View())
		}
	}
}

// TestNoRemoteDoesNotGetTheCleanCheckmark guards against a repository with
// no remote borrowing the everything-is-fine glyph: it is the one state
// with no copy anywhere else.
func TestNoRemoteDoesNotGetTheCleanCheckmark(t *testing.T) {
	noRemote := tuiItem{status: &vcs.RepoStatus{Type: vcs.Git}}
	if got := noRemote.icon(); got == "✅" {
		t.Fatalf("a repo with no remote must not show the clean checkmark, got %q", got)
	}
	backedUp := tuiItem{status: &vcs.RepoStatus{Type: vcs.Git, Remote: true, Ahead: vcs.Count{Known: true}}}
	if got := backedUp.icon(); got != "✅" {
		t.Fatalf("a clean repo with a remote should show the checkmark, got %q", got)
	}
}

// TestDirectoryHasNoStatusBadge guards the separation the badge column
// exists for: it reports status, never type.
func TestDirectoryHasNoStatusBadge(t *testing.T) {
	dir := tuiItem{status: &vcs.RepoStatus{Type: vcs.Dir, NestedRepos: 2}}
	if got := dir.icon(); got != "" {
		t.Fatalf("a directory must not occupy the status badge column, got %q", got)
	}
	if got := dir.typeText(); got != "dir" {
		t.Fatalf("type belongs in the type column, got %q", got)
	}
}
