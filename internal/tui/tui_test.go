package tui

import (
	"errors"
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
	got := item.icon()
	if !strings.Contains(got, glyphDirty) || !strings.Contains(got, glyphAhead) {
		t.Fatalf("expected dirty and ahead badges, got %q", got)
	}
	if lipgloss.Width(got) != badgeSlots {
		t.Fatalf("badges must occupy exactly %d fixed slots, got %q", badgeSlots, got)
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
	if got := item.icon(); !strings.Contains(got, glyphError) {
		t.Fatalf("expected the error glyph for an errored repo, got %q", got)
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
	if got := noRemote.icon(); strings.Contains(got, glyphClean) {
		t.Fatalf("a repo with no remote must not show the clean checkmark, got %q", got)
	}
	backedUp := tuiItem{status: &vcs.RepoStatus{Type: vcs.Git, Remote: true, Ahead: vcs.Count{Known: true}}}
	if got := backedUp.icon(); !strings.Contains(got, glyphClean) {
		t.Fatalf("a clean repo with a remote should show the checkmark, got %q", got)
	}
}

// TestDirectoryHasNoStatusBadge guards the separation the badge column
// exists for: it reports status, never type.
func TestDirectoryHasNoStatusBadge(t *testing.T) {
	dir := tuiItem{status: &vcs.RepoStatus{Type: vcs.Dir, NestedRepos: 2}}
	if got := strings.TrimSpace(dir.icon()); got != "" {
		t.Fatalf("a directory must not occupy the status badge column, got %q", got)
	}
	if got := dir.typeIcon(); got != "📁" {
		t.Fatalf("a directory belongs in the type column, got %q", got)
	}
}

func TestRollupText(t *testing.T) {
	// Only states actually present appear, so a quiet subtree stays quiet.
	cases := []struct {
		name string
		r    rollup
		want string
	}{
		{"empty", rollup{}, ""},
		{"clean subtree", rollup{repos: 4}, "4 repos"},
		{"singular", rollup{repos: 1}, "1 repo"},
		{"mixed", rollup{repos: 14, dirty: 3, ahead: 8}, "14 repos, 3 dirty, 8 ahead"},
		{"behind too", rollup{repos: 2, behind: 1}, "2 repos, 1 behind"},
	}
	for _, tc := range cases {
		if got := tc.r.text(); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestContainerShowsRollupWhenItArrives(t *testing.T) {
	m := tuiModelWithDirs(t, 1)
	path := m.items[0].path

	// Before the subtree scan lands the row falls back to the shallow
	// count, so it is never blank.
	updated, _ := m.Update(statusMsg{path: path, status: &vcs.RepoStatus{Path: path, Type: vcs.Dir, NestedRepos: 3}})
	um := updated.(model)
	if got := um.items[0].statusText(); got != "3 repos" {
		t.Fatalf("expected the shallow count before rollup, got %q", got)
	}

	updated, _ = um.Update(rollupMsg{path: path, r: rollup{repos: 3, dirty: 2, ahead: 1}})
	um = updated.(model)
	if got := um.items[0].statusText(); got != "3 repos, 2 dirty, 1 ahead" {
		t.Fatalf("expected the rollup once it arrives, got %q", got)
	}
}

func TestStatusMsgStartsRollupForContainersOnly(t *testing.T) {
	m := tuiModelWithDirs(t, 2)
	// A container schedules its subtree scan...
	_, cmd := m.Update(statusMsg{path: m.items[0].path, status: &vcs.RepoStatus{Type: vcs.Dir}})
	if cmd == nil {
		t.Fatal("expected a container to schedule a rollup")
	}
	// ...a repository has nothing to roll up.
	_, cmd = m.Update(statusMsg{path: m.items[1].path, status: &vcs.RepoStatus{Type: vcs.Git, Remote: true}})
	if cmd != nil {
		t.Fatal("a repository must not schedule a rollup")
	}
}

func TestTotalAggregatesWithoutDoubleCounting(t *testing.T) {
	m := tuiModelWithDirs(t, 3)
	// A plain repository, a rolled-up container, and a container whose
	// children are also present as expanded rows.
	m.items[0].status = &vcs.RepoStatus{Type: vcs.Git, Remote: true, Dirty: true}
	m.items[1].status = &vcs.RepoStatus{Type: vcs.Dir}
	m.items[1].roll = rollup{repos: 4, dirty: 1, ahead: 2}
	m.items[1].rolledUp = true
	m.items[2].status = &vcs.RepoStatus{Type: vcs.Dir}
	m.items[2].roll = rollup{repos: 2, ahead: 1}
	m.items[2].rolledUp = true
	// An expanded child must not be counted again on top of its parent's
	// rollup.
	m.items = append(m.items, tuiItem{
		path:   filepath.Join(m.items[2].path, "child"),
		depth:  1,
		status: &vcs.RepoStatus{Type: vcs.Git, Remote: true, Dirty: true},
	})

	tot, complete := m.total()
	if !complete {
		t.Fatal("expected the total to be complete")
	}
	want := rollup{repos: 7, dirty: 2, ahead: 3}
	if tot != want {
		t.Fatalf("total = %+v, want %+v", tot, want)
	}
}

func TestTotalIsIncompleteUntilRollupsLand(t *testing.T) {
	m := tuiModelWithDirs(t, 2)
	m.items[0].status = &vcs.RepoStatus{Type: vcs.Git, Remote: true}
	m.items[1].status = &vcs.RepoStatus{Type: vcs.Dir} // rollup pending
	if _, complete := m.total(); complete {
		t.Fatal("expected the total to report itself incomplete while a rollup is pending")
	}
}

// nameColumnOffsets returns the screen column each row's name starts at.
func nameColumnOffsets(t *testing.T, m model) []int {
	t.Helper()
	var cols []int
	for _, line := range strings.Split(m.View(), "\n") {
		plain := stripANSI(line)
		// Skip the totals line, whose figures also mention "repos".
		if strings.Contains(plain, "Total") || !strings.Contains(plain, "repo") {
			continue
		}
		cols = append(cols, lipgloss.Width(plain[:strings.Index(plain, "repo")]))
	}
	return cols
}

// TestColumnsDoNotShiftAsStatusesArrive is the regression for columns
// jumping between the initial render and the loaded one. Widths must come
// from data known at listing time, never from the badges, which change as
// background scans land.
func TestColumnsDoNotShiftAsStatusesArrive(t *testing.T) {
	forceColorProfile(t)
	// Names of differing lengths, so a width derived from any subset of
	// the rows would differ from one derived from all of them.
	tmpDir, err := os.MkdirTemp("", "tui-cols")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	for _, n := range []string{"repo0-x", "repo1-medium-name", "repo2-considerably-longer-name", "repo3"} {
		if err := os.Mkdir(filepath.Join(tmpDir, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m, err := newModel(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	um := updated.(model)

	before := nameColumnOffsets(t, um)
	widthBefore := um.prefixWidth()
	if len(before) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(before))
	}

	// Statuses arrive with badges of every shape, plus differing types.
	um.items[0].status = &vcs.RepoStatus{Type: vcs.Git, Remote: true, Ahead: vcs.Count{Known: true}}
	um.items[1].status = &vcs.RepoStatus{Type: vcs.Jujutsu, Remote: true, Dirty: true, Ahead: vcs.Count{N: 3, Known: true}}
	um.items[2].status = &vcs.RepoStatus{Type: vcs.Dir, NestedRepos: 9}
	um.items[3].status = &vcs.RepoStatus{Type: vcs.Git, Corrupted: true}

	// The status column sits after the padded name, so a name width that
	// moved would move every status with it.
	if got, want := um.prefixWidth(), widthBefore; got != want {
		t.Errorf("row prefix width changed from %d to %d once statuses arrived", want, got)
	}

	after := nameColumnOffsets(t, um)
	for i := range after {
		if after[i] != before[i] {
			t.Errorf("row %d name column moved from %d to %d when its status arrived:\n%s",
				i, before[i], after[i], um.View())
		}
	}
	for i := range after {
		if after[i] != after[0] {
			t.Errorf("row %d name column at %d, row 0 at %d:\n%s", i, after[i], after[0], um.View())
		}
	}
}

// TestSyncGlyphHoldsItsColumn is the regression for the up arrows not
// lining up: a glyph must sit in the same slot whether or not the slots
// before it are occupied.
func TestSyncGlyphHoldsItsColumn(t *testing.T) {
	aheadOnly := tuiItem{status: &vcs.RepoStatus{Type: vcs.Git, Remote: true, Ahead: vcs.Count{N: 1, Known: true}}}
	dirtyAndAhead := tuiItem{status: &vcs.RepoStatus{Type: vcs.Git, Remote: true, Dirty: true, Ahead: vcs.Count{N: 1, Known: true}}}

	// Compare slot positions, not byte offsets: the glyphs are multi-byte
	// but each occupies exactly one slot.
	slotOf := func(badges, glyph string) int {
		for i, r := range []rune(badges) {
			if string(r) == glyph {
				return i
			}
		}
		return -1
	}
	a, b := aheadOnly.icon(), dirtyAndAhead.icon()
	if slotOf(a, glyphAhead) != slotOf(b, glyphAhead) {
		t.Fatalf("ahead glyph sits in slot %d of %q but slot %d of %q",
			slotOf(a, glyphAhead), a, slotOf(b, glyphAhead), b)
	}
	if lipgloss.Width(a) != lipgloss.Width(b) {
		t.Fatalf("badge columns differ in width: %q vs %q", a, b)
	}
}

func TestTotalsLineCountsItems(t *testing.T) {
	r := rollup{repos: 160, dirty: 71, ahead: 81}
	if got := r.itemText(27); got != "27 items, 160 repos, 71 dirty, 81 ahead" {
		t.Fatalf("got %q", got)
	}
	if got := (rollup{}).itemText(1); got != "1 item" {
		t.Fatalf("got %q", got)
	}
}

func TestEscHidesExpandedHelp(t *testing.T) {
	m := tuiModelWithDirs(t, 2)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	um := updated.(model)
	if !um.help.ShowAll {
		t.Fatal("expected ? to expand the help")
	}

	updated, _ = um.Update(tea.KeyMsg{Type: tea.KeyEsc})
	um = updated.(model)
	if um.help.ShowAll {
		t.Fatal("expected esc to hide the expanded help")
	}

	// Esc is a dismissal only: with the help already collapsed it must not
	// swallow the key or toggle anything back on.
	updated, _ = um.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(model).help.ShowAll {
		t.Fatal("esc must not re-expand the help")
	}
}

// TestDetailColumnHoldsAcrossDepths is the regression for indenting the
// glyphs: a child's icons shift right with the nesting, but the detail
// column must not move with them.
func TestDetailColumnHoldsAcrossDepths(t *testing.T) {
	forceColorProfile(t)
	m := tuiModelWithDirs(t, 2)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	um := updated.(model)
	um.items[0].status = &vcs.RepoStatus{Type: vcs.Git, Remote: true, Ahead: vcs.Count{N: 1, Known: true}}
	um.items[1].status = &vcs.RepoStatus{Type: vcs.Dir}
	um.items[1].rolledUp = true
	um.items[1].roll = rollup{repos: 1}
	// A nested child, indented one level.
	um.items = append(um.items, tuiItem{
		path:   filepath.Join(um.items[1].path, "child"),
		depth:  1,
		status: &vcs.RepoStatus{Type: vcs.Git, Remote: true, Ahead: vcs.Count{N: 1, Known: true}},
	})

	var cols []int
	for _, line := range strings.Split(um.View(), "\n") {
		plain := stripANSI(line)
		i := strings.Index(plain, "ahead 1")
		if i < 0 {
			continue
		}
		cols = append(cols, lipgloss.Width(plain[:i]))
	}
	if len(cols) != 2 {
		t.Fatalf("expected two rows showing \"ahead 1\", got %d:\n%s", len(cols), um.View())
	}
	if cols[0] != cols[1] {
		t.Fatalf("detail column moved with the indent: %d vs %d:\n%s", cols[0], cols[1], um.View())
	}
}

func TestDraftSeedsOnlyAnUntouchedPrompt(t *testing.T) {
	m := tuiModelWithDirs(t, 1)
	path := m.items[0].path
	m.draftPath = path
	m.drafting = true
	_ = m.commitInput.Focus()
	m.commitInput.SetValue(actions.DefaultCommitMessage)

	updated, _ := m.Update(draftMsg{path: path, message: "feat: drafted by the tool"})
	um := updated.(model)
	if um.commitInput.Value() != "feat: drafted by the tool" {
		t.Fatalf("expected the draft to seed an untouched prompt, got %q", um.commitInput.Value())
	}
	if um.drafting {
		t.Fatal("expected drafting to finish")
	}

	// A prompt the user has begun editing must not be overwritten.
	m2 := tuiModelWithDirs(t, 1)
	p2 := m2.items[0].path
	m2.draftPath = p2
	m2.drafting = true
	_ = m2.commitInput.Focus()
	m2.commitInput.SetValue("my own words")
	updated, _ = m2.Update(draftMsg{path: p2, message: "feat: drafted by the tool"})
	if got := updated.(model).commitInput.Value(); got != "my own words" {
		t.Fatalf("a draft must not overwrite the user's typing, got %q", got)
	}
}

func TestDraftFailureKeepsTheCannedDefault(t *testing.T) {
	m := tuiModelWithDirs(t, 1)
	path := m.items[0].path
	m.draftPath = path
	m.drafting = true
	_ = m.commitInput.Focus()
	m.commitInput.SetValue(actions.DefaultCommitMessage)

	updated, _ := m.Update(draftMsg{path: path, err: errors.New("tool exploded")})
	um := updated.(model)
	if um.commitInput.Value() != actions.DefaultCommitMessage {
		t.Fatalf("a failed draft must leave the prompt usable, got %q", um.commitInput.Value())
	}
	if um.drafting {
		t.Fatal("expected drafting to finish even on failure")
	}
	// An empty draft is treated the same way.
	updated, _ = m.Update(draftMsg{path: path, message: "   "})
	if got := updated.(model).commitInput.Value(); got != actions.DefaultCommitMessage {
		t.Fatalf("an empty draft must leave the canned default, got %q", got)
	}
}

func TestDraftForAnotherRepositoryIsIgnored(t *testing.T) {
	m := tuiModelWithDirs(t, 2)
	m.draftPath = m.items[0].path
	m.drafting = true
	_ = m.commitInput.Focus()
	m.commitInput.SetValue(actions.DefaultCommitMessage)

	// A draft that arrives after the prompt moved on must not land.
	updated, _ := m.Update(draftMsg{path: m.items[1].path, message: "feat: wrong repo"})
	um := updated.(model)
	if um.commitInput.Value() != actions.DefaultCommitMessage {
		t.Fatalf("a stale draft must be ignored, got %q", um.commitInput.Value())
	}
	if !um.drafting {
		t.Fatal("a stale draft must not clear the pending state")
	}
}
