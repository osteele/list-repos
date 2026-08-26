package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/osteele/gitsync/internal/actions"
	"github.com/osteele/gitsync/internal/vcs"
)

func TestBulkProgressUsesActiveVerbsAndCompletionCounts(t *testing.T) {
	for _, tc := range []struct {
		op   actions.BulkOp
		verb string
	}{
		{actions.BulkCommit, "Committing"},
		{actions.BulkPull, "Pulling"},
		{actions.BulkFix, "Fixing"},
		{actions.BulkPush, "Pushing"},
		{actions.BulkSync, "Syncing"},
	} {
		started := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
		progress := bulkProgressState{
			op:        tc.op,
			total:     7,
			completed: 3,
			failed:    1,
			started:   started,
			running: map[string]string{
				"/work/agent-lore": "agent-lore",
				"/work/agent-mail": "agent-mail",
				"/work/gitsync":    "gitsync",
			},
		}
		got := progress.message(started.Add(12 * time.Second))
		for _, want := range []string{tc.verb + " 7 repositories…", "3/7 complete", "1 failed", "3 running: agent-lore, agent-mail +1", "1 queued", "12s"} {
			if !strings.Contains(got, want) {
				t.Fatalf("%s progress missing %q: %q", tc.verb, want, got)
			}
		}
	}
}

func TestBulkConfirmationVerbsAreCapitalized(t *testing.T) {
	for _, tc := range []struct {
		op   actions.BulkOp
		want string
	}{
		{actions.BulkCommit, "Commit"},
		{actions.BulkPull, "Pull"},
		{actions.BulkFix, "Fix"},
		{actions.BulkPush, "Push"},
		{actions.BulkSync, "Sync"},
	} {
		if got := bulkPromptVerb(tc.op); got != tc.want {
			t.Fatalf("bulkPromptVerb(%s) = %q, want %q", tc.op, got, tc.want)
		}
	}
}

func TestBulkStartedMessageMovesRepositoryFromQueuedToRunning(t *testing.T) {
	m := tuiModelWithDirs(t, 1)
	path := m.items[0].path
	m.bulkProgress = &bulkProgressState{
		runID:          4,
		op:             actions.BulkPush,
		total:          2,
		started:        time.Now(),
		running:        make(map[string]string),
		completedPaths: make(map[string]struct{}),
	}
	ch := make(chan actions.PlanItem)
	close(ch)
	updated, cmd := m.Update(bulkStartedMsg{
		runID: 4,
		item:  actions.PlanItem{Status: &vcs.RepoStatus{Path: path}},
		ch:    ch,
	})
	um := updated.(model)
	if cmd == nil || !strings.Contains(um.message, "1 running: "+filepath.Base(path)) || !strings.Contains(um.message, "1 queued") {
		t.Fatalf("actual worker start was not reflected in progress: %q", um.message)
	}
}

func TestRepositoryActionsOnDirectoryPlanSelectedScope(t *testing.T) {
	for _, tc := range []struct {
		key, action string
		op          actions.BulkOp
	}{
		{"p", "push", actions.BulkPush},
		{"u", "pull", actions.BulkPull},
		{"s", "sync", actions.BulkSync},
		{"f", "fix", actions.BulkFix},
	} {
		t.Run(tc.key, func(t *testing.T) {
			m := tuiModelWithDirs(t, 1)
			scope := m.items[0].path
			m.items[0].status = &vcs.RepoStatus{Path: scope, Type: vcs.Dir}

			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.key)})
			um := updated.(model)
			if cmd == nil || !um.items[0].busy || um.pending != nil {
				t.Fatalf("%s did not scan the selected directory: busy=%v pending=%v", tc.key, um.items[0].busy, um.pending != nil)
			}
			want := "planning " + tc.action + " in " + filepath.Base(scope) + "…"
			if um.message != want {
				t.Fatalf("directory hint = %q, want %q", um.message, want)
			}
			plan, ok := cmd().(bulkPlanMsg)
			if !ok || plan.op != tc.op || plan.scope != scope {
				t.Fatalf("%s plan = %#v", tc.key, plan)
			}
		})
	}
}

func TestFixBindingAndRepositoryBehavior(t *testing.T) {
	if got := strings.Join(defaultKeyMap.Fix.Keys(), ","); got != "f" {
		t.Fatalf("fix keys = %q", got)
	}
	if got := strings.Join(defaultKeyMap.Reveal.Keys(), ","); got != "v" {
		t.Fatalf("reveal keys = %q", got)
	}

	m := tuiModelWithDirs(t, 1)
	m.items[0].status = &vcs.RepoStatus{Path: m.items[0].path, Type: vcs.Git}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	um := updated.(model)
	if cmd == nil || um.items[0].busy || !strings.Contains(um.message, "requires a Jujutsu repository") {
		t.Fatalf("f on Git was not refused: busy=%v message=%q", um.items[0].busy, um.message)
	}

	m.items[0].status = &vcs.RepoStatus{Path: m.items[0].path, Type: vcs.Jujutsu}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	um = updated.(model)
	if cmd == nil || !um.items[0].busy || um.message != "fix: repo00…" {
		t.Fatalf("f on Jujutsu did not start: busy=%v message=%q", um.items[0].busy, um.message)
	}
}

func TestFixOutputRemainsAvailableInDetails(t *testing.T) {
	m := tuiModelWithDirs(t, 1)
	path := m.items[0].path
	m.items[0].status = &vcs.RepoStatus{Path: path, Type: vcs.Jujutsu}
	updated, cmd := m.runOutputActionAt("fix", path, func(string) (string, error) {
		return "Review: jj -R /work/repo op show -p abc123", nil
	})
	um := updated.(model)
	updated, _ = um.Update(cmd())
	um = updated.(model)
	if !strings.Contains(um.items[0].lastResult, "op show -p abc123") {
		t.Fatalf("fix review command was lost: %q", um.items[0].lastResult)
	}
}

func TestScopedPushConfirmationNamesSelectedDirectory(t *testing.T) {
	m := tuiModelWithDirs(t, 1)
	scope := m.items[0].path
	m.items[0].status = &vcs.RepoStatus{Path: scope, Type: vcs.Dir}
	eligible := &vcs.RepoStatus{
		Path:   filepath.Join(scope, "child"),
		Type:   vcs.Git,
		Remote: true,
		Ahead:  vcs.Count{N: 1, Known: true},
	}

	updated, _ := m.Update(bulkPlanMsg{op: actions.BulkPush, scope: scope, statuses: []*vcs.RepoStatus{eligible}})
	um := updated.(model)
	if um.pending == nil || !strings.Contains(um.pending.prompt, "Push 1 repository in "+filepath.Base(scope)) {
		t.Fatalf("scoped confirmation = %v", um.pending)
	}
}

func TestUppercaseBulkPlanIncludesCollapsedRepositories(t *testing.T) {
	m := tuiModelWithDirs(t, 1)
	container := m.items[0].path
	repo := filepath.Join(container, "child")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	m.items[0].status = &vcs.RepoStatus{Path: container, Type: vcs.Dir}
	if m.items[0].expanded {
		t.Fatal("fixture must leave the directory collapsed")
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	um := updated.(model)
	if cmd == nil || um.message != "planning push for all repositories…" {
		t.Fatalf("uppercase P did not scan the full scope: %q", um.message)
	}
	plan, ok := cmd().(bulkPlanMsg)
	if !ok || plan.err != nil {
		t.Fatalf("bulk scan = %#v", plan)
	}
	for _, status := range plan.statuses {
		if status.Path == repo && status.Type == vcs.Git {
			return
		}
	}
	t.Fatalf("collapsed repository %s missing from bulk plan: %#v", repo, plan.statuses)
}

func TestCompletedActionRefreshesAncestorRollupAndTotal(t *testing.T) {
	m := tuiModelWithDirs(t, 1)
	parent := m.items[0].path
	child := filepath.Join(parent, "child")
	m.items[0].status = &vcs.RepoStatus{Path: parent, Type: vcs.Dir}
	m.items[0].rolledUp = true
	m.items[0].rollupSeq = 4
	m.items[0].roll = rollup{repos: 1, direct: 1, ahead: 1}
	m.items = append(m.items, tuiItem{
		path:   child,
		depth:  1,
		status: &vcs.RepoStatus{Path: child, Type: vcs.Git, Remote: true, Ahead: vcs.Count{N: 1, Known: true}},
	})

	updated, cmd := m.Update(actionDoneMsg{path: child, action: "push", message: "push child done"})
	um := updated.(model)
	if cmd == nil || um.items[0].rolledUp {
		t.Fatal("completed child action did not invalidate its ancestor rollup")
	}
	if _, complete := um.total(); complete {
		t.Fatal("total claimed to be current while its ancestor rollup was refreshing")
	}
	if um.items[0].rollupSeq != 5 {
		t.Fatalf("rollup sequence = %d, expected 5", um.items[0].rollupSeq)
	}

	updated, _ = um.Update(rollupMsg{path: parent, seq: 5, r: rollup{repos: 1, direct: 1}})
	um = updated.(model)
	total, complete := um.total()
	if !complete || total.ahead != 0 {
		t.Fatalf("refreshed total = %+v, complete=%v", total, complete)
	}

	// A pre-action scan that finishes late cannot restore the stale count.
	updated, _ = um.Update(rollupMsg{path: parent, seq: 4, r: rollup{repos: 1, direct: 1, ahead: 1}})
	um = updated.(model)
	if um.items[0].roll.ahead != 0 {
		t.Fatal("stale rollup result overwrote the post-action aggregate")
	}
}

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

	statuses := []*vcs.RepoStatus{m.items[0].status, m.items[1].status}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	um := updated.(model)
	if cmd == nil || um.message != "planning push for all repositories…" {
		t.Fatalf("expected an asynchronous full-scope plan, got %q", um.message)
	}
	updated, _ = um.Update(bulkPlanMsg{op: actions.BulkPush, statuses: statuses})
	um = updated.(model)
	if um.pending == nil {
		t.Fatal("expected push-all to await confirmation")
	}
	if !strings.Contains(um.pending.prompt, "Push 2 repositories") {
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

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	um := updated.(model)
	if cmd == nil {
		t.Fatal("expected an asynchronous full-scope plan")
	}
	updated, _ = um.Update(bulkPlanMsg{op: actions.BulkPush, statuses: []*vcs.RepoStatus{m.items[0].status}})
	um = updated.(model)
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
	if !strings.Contains(um.message, "Push: 2 repositories succeeded; 1 failed") {
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

func TestBulkFailureReportOpensAndStaysOnScopedDirectory(t *testing.T) {
	m := tuiModelWithDirs(t, 1)
	resized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = resized.(model)
	scope := m.items[0].path
	m.items[0].status = &vcs.RepoStatus{Path: scope, Type: vcs.Dir}

	updated, _ := m.Update(bulkDoneMsg{
		op:        actions.BulkPush,
		scope:     scope,
		succeeded: 4,
		failed:    1,
		failures: []bulkFailure{{
			path:     filepath.Join(scope, "agent-lore"),
			repoType: vcs.Jujutsu,
			message:  "push failed: remote rejected main\nhint: inspect the bookmark",
		}},
	})
	um := updated.(model)
	if !um.showDetail {
		t.Fatal("a failed bulk action should open its failure report")
	}
	for _, want := range []string{
		"Push: 4 repositories succeeded; 1 failed",
		"Failure 1 of 1",
		"Repository: agent-lore",
		"Location: " + filepath.Join(scope, "agent-lore"),
		"Version control: Jujutsu",
		"Error:",
		"remote rejected main",
		"inspect the bookmark",
	} {
		if !strings.Contains(um.detail.View(), want) {
			t.Fatalf("failure report missing %q:\n%s", want, um.detail.View())
		}
		if !strings.Contains(um.items[0].lastResult, want) {
			t.Fatalf("scoped directory result missing %q:\n%s", want, um.items[0].lastResult)
		}
	}
}

func TestBulkFixSummaryReportsRevisionsAcrossRepositories(t *testing.T) {
	m := tuiModelWithDirs(t, 1)
	updated, _ := m.Update(bulkDoneMsg{
		op:               actions.BulkFix,
		succeeded:        4,
		failed:           1,
		changedRevisions: 7,
		changedRepos:     3,
	})
	um := updated.(model)
	if um.message != "Fix: 7 revisions changed across 3 repositories; 1 failed" {
		t.Fatalf("unexpected fix summary %q", um.message)
	}
}
