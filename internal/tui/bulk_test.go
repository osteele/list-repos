package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/osteele/gitsync/internal/actions"
	"github.com/osteele/gitsync/internal/vcs"
)

func TestBulkPushRequiresConfirmation(t *testing.T) {
	m := tuiModelWithDirs(t, 2)
	for i := range m.items {
		m.items[i].status = &vcs.RepoStatus{
			Path:   m.items[i].path,
			Type:   vcs.Git,
			Remote: true,
			Ahead:  vcs.Count{N: 1, Known: true},
		}
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	um := updated.(model)
	if um.pending == nil {
		t.Fatal("expected push-all to await confirmation")
	}
	if !strings.Contains(um.pending.prompt, "push 2 repositories") {
		t.Fatalf("expected a count in the prompt, got %q", um.pending.prompt)
	}
	for _, item := range um.items {
		if item.busy {
			t.Fatal("nothing may be busy before confirmation")
		}
	}

	// Confirming marks every eligible repository busy.
	updated, _ = um.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	um = updated.(model)
	if um.pending != nil {
		t.Fatal("expected y to accept the confirmation")
	}
	for _, item := range um.items {
		if !item.busy {
			t.Fatalf("expected %s busy while the batch runs", item.name())
		}
	}
}

func TestBulkPushNothingEligible(t *testing.T) {
	m := tuiModelWithDirs(t, 1)
	m.items[0].status = &vcs.RepoStatus{
		Path:   m.items[0].path,
		Type:   vcs.Git,
		Remote: true,
		Ahead:  vcs.Count{N: 0, Known: true},
		Behind: vcs.Count{N: 0, Known: true},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	um := updated.(model)
	if um.pending != nil {
		t.Fatal("an empty plan must not ask for confirmation")
	}
	if um.message != actions.EmptyMessage(actions.BulkPush) {
		t.Fatalf("expected the empty message, got %q", um.message)
	}
}

func TestBulkResultClearsBusyRow(t *testing.T) {
	m := tuiModelWithDirs(t, 1)
	m.items[0].busy = true
	status := &vcs.RepoStatus{Path: m.items[0].path, Type: vcs.Git, Remote: true, Ahead: vcs.Count{N: 1, Known: true}}
	m.items[0].status = status

	ch := make(chan actions.Result, 1)
	close(ch)
	msg := bulkResultMsg{
		op:     actions.BulkPush,
		result: actions.Result{Item: actions.PlanItem{Status: status, Reason: "ahead 1"}},
		ch:     ch,
	}
	updated, cmd := m.Update(msg)
	um := updated.(model)
	if um.items[0].busy {
		t.Fatal("a finished repository must not stay busy")
	}
	if !strings.Contains(um.items[0].lastResult, "push") {
		t.Fatalf("expected a last result, got %q", um.items[0].lastResult)
	}
	if cmd == nil {
		t.Fatal("expected a command awaiting the rest of the batch")
	}
}

func TestBulkDoneSummaryIsStickyOnFailure(t *testing.T) {
	m := tuiModelWithDirs(t, 1)

	updated, _ := m.Update(bulkDoneMsg{op: actions.BulkPush, succeeded: 2, failed: 1})
	um := updated.(model)
	if !strings.Contains(um.message, "2 pushed, 1 failed") {
		t.Fatalf("expected a summary, got %q", um.message)
	}
	if !um.messageSticky {
		t.Fatal("a batch with failures must stay on screen")
	}

	updated, _ = m.Update(bulkDoneMsg{op: actions.BulkPush, succeeded: 3, failed: 0})
	um = updated.(model)
	if um.messageSticky {
		t.Fatal("a clean sweep must not stick")
	}
}
