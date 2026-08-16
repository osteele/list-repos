package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/osteele/gitsync/internal/vcs"
)

// tuiModelWithContainer builds a model over a container directory holding
// two plain subdirectories, with the container's status already loaded.
func tuiModelWithContainer(t *testing.T) model {
	t.Helper()
	tmpDir := t.TempDir()
	container := filepath.Join(tmpDir, "container")
	for _, child := range []string{"alpha", "beta"} {
		if err := os.MkdirAll(filepath.Join(container, child), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m, err := newModel(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := m.Update(statusMsg{path: container, status: &vcs.RepoStatus{Path: container, Type: vcs.Dir, NestedRepos: 2}})
	return updated.(model)
}

func TestExpandCollapse(t *testing.T) {
	m := tuiModelWithContainer(t)
	if len(m.items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(m.items))
	}
	if !m.items[0].expandable() {
		t.Fatal("a non-repository directory must be expandable")
	}

	// → starts a lazy scan: the row shows its busy state while it runs.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("expected the first expansion to scan the children")
	}
	if !m.items[0].busy {
		t.Fatal("expected the row's busy state while children load")
	}

	msg := cmd()
	children, ok := msg.(childrenMsg)
	if !ok {
		t.Fatalf("expected childrenMsg, got %T", msg)
	}
	if len(children.statuses) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children.statuses))
	}

	updated, _ = m.Update(children)
	m = updated.(model)
	if len(m.items) != 3 {
		t.Fatalf("expected 3 items after expansion, got %d", len(m.items))
	}
	if m.items[0].busy {
		t.Fatal("expected the busy state to clear once children arrive")
	}
	if !m.items[0].expanded {
		t.Fatal("expected the container to be marked expanded")
	}
	if m.items[1].depth != 1 || m.items[2].depth != 1 {
		t.Fatalf("expected children at depth 1, got %d and %d", m.items[1].depth, m.items[2].depth)
	}
	if got := m.items[1].name(); got != "alpha" {
		t.Fatalf("expected children sorted by name, got %q first", got)
	}

	// ← collapses; the children stay cached.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(model)
	if len(m.items) != 1 {
		t.Fatalf("expected 1 item after collapse, got %d", len(m.items))
	}
	if m.items[0].expanded {
		t.Fatal("expected the container to be marked collapsed")
	}

	// Re-expanding uses the cache instead of rescanning.
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(model)
	if cmd != nil {
		t.Fatal("expected the second expansion to reuse the cached children")
	}
	if len(m.items) != 3 {
		t.Fatalf("expected 3 items after re-expansion, got %d", len(m.items))
	}
}

func TestExpandRenderMarkersAndIndent(t *testing.T) {
	m := tuiModelWithContainer(t)
	m.width = 80
	m.height = 20

	view := m.View()
	if !strings.Contains(view, "▸ container") {
		t.Fatalf("expected a collapsed marker before the container name:\n%s", view)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("expected l to expand like →")
	}
	updated, _ = m.Update(cmd())
	m = updated.(model)

	view = m.View()
	if !strings.Contains(view, "▾ container") {
		t.Fatalf("expected an expanded marker before the container name:\n%s", view)
	}
	if !strings.Contains(view, "  alpha") {
		t.Fatalf("expected children indented under the container:\n%s", view)
	}

	// h collapses like ←.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = updated.(model)
	if len(m.items) != 1 {
		t.Fatalf("expected h to collapse like ←, got %d items", len(m.items))
	}
}

func TestExpandKeepsCursorInViewport(t *testing.T) {
	m := tuiModelWithDirs(t, 40)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: chromeHeight + 5})
	m = updated.(model)

	// Make one row mid-list a loaded container holding children.
	container := m.items[20].path
	for i := 0; i < 5; i++ {
		if err := os.Mkdir(filepath.Join(container, fmt.Sprintf("child%d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	updated, _ = m.Update(statusMsg{path: container, status: &vcs.RepoStatus{Path: container, Type: vcs.Dir, NestedRepos: 5}})
	m = updated.(model)

	for i := 0; i < 20; i++ {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(model)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("expected the expansion to scan the children")
	}
	updated, _ = m.Update(cmd())
	m = updated.(model)

	if len(m.items) != 45 {
		t.Fatalf("expected 45 items after expansion, got %d", len(m.items))
	}
	if m.cursor != 20 {
		t.Fatalf("expected the cursor to stay on the expanded row, got %d", m.cursor)
	}
	if m.cursor < m.offset || m.cursor >= m.offset+m.listHeight() {
		t.Fatalf("cursor %d outside window [%d,%d)", m.cursor, m.offset, m.offset+m.listHeight())
	}

	// Navigation continues into the children.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if m.cursor != 21 || m.items[m.cursor].depth != 1 {
		t.Fatalf("expected the cursor to move into the children, got index %d at depth %d", m.cursor, m.items[m.cursor].depth)
	}
	if m.cursor < m.offset || m.cursor >= m.offset+m.listHeight() {
		t.Fatalf("cursor %d outside window [%d,%d)", m.cursor, m.offset, m.offset+m.listHeight())
	}
}
