package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/osteele/gitsync/internal/vcs"
)

func TestStatusTokensNestedRepos(t *testing.T) {
	// A directory that contains repositories says so; an empty directory
	// keeps an empty status.
	status := &vcs.RepoStatus{Type: vcs.Dir, NestedRepos: 3}
	tokens := StatusTokens(status)
	if len(tokens) != 1 || tokens[0].Text != "3 repos" {
		t.Errorf("StatusTokens(3 nested) = %+v, expected \"3 repos\"", tokens)
	}

	status = &vcs.RepoStatus{Type: vcs.Dir, NestedRepos: 1}
	tokens = StatusTokens(status)
	if len(tokens) != 1 || tokens[0].Text != "1 repo" {
		t.Errorf("StatusTokens(1 nested) = %+v, expected \"1 repo\"", tokens)
	}

	if tokens := StatusTokens(&vcs.RepoStatus{Type: vcs.Dir}); len(tokens) != 0 {
		t.Errorf("StatusTokens(empty dir) = %+v, expected none", tokens)
	}

	if got := statusText(&vcs.RepoStatus{Type: vcs.Dir, NestedRepos: 2}, false); got != "2 repos" {
		t.Errorf("statusText(2 nested) = %q, expected \"2 repos\"", got)
	}
}

func TestPrepareDisplayKeepsContextParents(t *testing.T) {
	repo := &vcs.RepoStatus{Path: "/r/repo", Type: vcs.Git, Remote: true, Ahead: vcs.Count{Known: true}, Depth: 1}
	container := &vcs.RepoStatus{Path: "/r/container", Type: vcs.Dir, NestedRepos: 1, Depth: 1}
	child := &vcs.RepoStatus{Path: "/r/container/child", Type: vcs.Git, Remote: true, Ahead: vcs.Count{Known: true}, Depth: 2}
	empty := &vcs.RepoStatus{Path: "/r/empty", Type: vcs.Dir, Depth: 1}
	// main marks a directory that contains repositories ContextOnly when no
	// filter is given, so it shows even when its children are not listed.
	lonely := &vcs.RepoStatus{Path: "/r/lonely", Type: vcs.Dir, NestedRepos: 5, Depth: 1, ContextOnly: true}
	results := []*vcs.RepoStatus{container, child, empty, lonely, repo}

	visible, hidden := PrepareDisplay(results, false)
	if len(visible) != 4 {
		t.Fatalf("expected the containers kept as context plus the two repos, got %d rows", len(visible))
	}
	if visible[0] != container || visible[1] != child || visible[2] != lonely || visible[3] != repo {
		t.Fatal("expected each container immediately before its child")
	}
	if !container.ContextOnly || !lonely.ContextOnly {
		t.Error("expected the containers to be marked context-only")
	}
	if repo.ContextOnly || child.ContextOnly {
		t.Error("repositories are never context-only")
	}
	if hidden != 1 {
		t.Errorf("expected 1 hidden non-repo (the empty directory), got %d", hidden)
	}

	// A context-only parent is not tallied in the summary.
	if got := summaryLine(visible, hidden); got != "2 repos (+1 non-repo hidden): all clean" {
		t.Errorf("summaryLine() = %q, expected the context parents untallied", got)
	}

	// With --all the containers are ordinary rows and tally as non-repos.
	visible, hidden = PrepareDisplay(results, true)
	if len(visible) != 5 || hidden != 0 {
		t.Fatalf("PrepareDisplay(_, true) kept %d rows with %d hidden, expected 5 and 0", len(visible), hidden)
	}
	if container.ContextOnly || lonely.ContextOnly {
		t.Error("with --all the containers are shown on their own merits, not as context")
	}
}

func TestPrintReportIndentsChildren(t *testing.T) {
	visible := []*vcs.RepoStatus{
		{Path: "/r/container", Type: vcs.Dir, NestedRepos: 1, Depth: 1, ContextOnly: true},
		{Path: "/r/container/child", Type: vcs.Git, Remote: true, Ahead: vcs.Count{Known: true}, Depth: 2},
	}

	var buf bytes.Buffer
	PrintReport(&buf, visible, 0, false)
	output := buf.String()

	if !strings.Contains(output, "container") || !strings.Contains(output, "1 repo") {
		t.Errorf("expected the container row with its nested count:\n%s", output)
	}
	var childLine string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "child") {
			childLine = line
		}
	}
	if !strings.HasPrefix(childLine, "  child") {
		t.Errorf("expected the child row indented under its parent, got %q", childLine)
	}
}
