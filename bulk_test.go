package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/osteele/gitsync/internal/actions"
	"github.com/osteele/gitsync/internal/vcs"
)

func TestBulkFlagsMutuallyExclusive(t *testing.T) {
	if _, err := parseArgs([]string{"--push-all", "--pull-all"}, io.Discard); err == nil {
		t.Fatal("expected --push-all with --pull-all to be a usage error")
	}
	if _, err := parseArgs([]string{"--commit-all", "--sync-all"}, io.Discard); err == nil {
		t.Fatal("expected --commit-all with --sync-all to be a usage error")
	}
	if _, err := parseArgs([]string{"--fix-all", "--push-all"}, io.Discard); err == nil {
		t.Fatal("expected --fix-all with --push-all to be a usage error")
	}
	if _, err := parseArgs([]string{"--push-all", "-i"}, io.Discard); err == nil {
		t.Fatal("expected --push-all with -i to be a usage error")
	}

	opts, err := parseArgs([]string{"--push-all", "--dry-run", "-y"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	op, ok := opts.bulkOp()
	if !ok || op != actions.BulkPush || !opts.dryRun || !opts.yes {
		t.Fatalf("unexpected options: %+v", opts)
	}
}

func TestRunBulkFixDryRunIsPlanOnly(t *testing.T) {
	var stdout, stderr bytes.Buffer
	results := []*vcs.RepoStatus{
		{Path: "/work/jj-project", Type: vcs.Jujutsu},
		{Path: "/work/git-project", Type: vcs.Git},
	}
	code := runBulk(actions.BulkFix, results, true, false, nonTTYStdin(t), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Will fix 1 repository:") || !strings.Contains(out, "jj-project") {
		t.Fatalf("expected a Jujutsu-only plan, got %q", out)
	}
	if strings.Contains(out, "git-project") || strings.Contains(out, "processed") {
		t.Fatalf("dry-run must neither select Git nor execute fix, got %q", out)
	}
}

// nonTTYStdin returns a pipe, which is not a terminal, standing in for a
// scripted invocation.
func nonTTYStdin(t *testing.T) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	return r
}

func TestRunBulkNothingEligible(t *testing.T) {
	var stdout, stderr bytes.Buffer
	results := []*vcs.RepoStatus{
		{Path: "/work/clean", Type: vcs.Git, Remote: true, Ahead: vcs.Count{N: 0, Known: true}, Behind: vcs.Count{N: 0, Known: true}},
	}
	code := runBulk(actions.BulkPush, results, false, false, nonTTYStdin(t), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Nothing to push.") {
		t.Fatalf("expected the empty message, got %q", stdout.String())
	}
}

func TestRunBulkNonTTYRefuses(t *testing.T) {
	var stdout, stderr bytes.Buffer
	results := []*vcs.RepoStatus{
		{Path: "/work/ahead", Type: vcs.Git, Remote: true, Ahead: vcs.Count{N: 1, Known: true}},
	}
	code := runBulk(actions.BulkPush, results, false, false, nonTTYStdin(t), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit 2, got %d", code)
	}
	if !strings.Contains(stdout.String(), "Will push 1 repository:") {
		t.Fatalf("the plan must print even when refusing, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--yes") {
		t.Fatalf("the refusal must explain the fix, got %q", stderr.String())
	}
}

// pushFixture builds a local repo with one unpushed commit against a bare
// remote on disk, and its scanned-looking status.
func pushFixture(t *testing.T, name string) (remoteDir string, status *vcs.RepoStatus) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)

	remoteDir = filepath.Join(t.TempDir(), name+".git")
	if err := exec.Command("git", "init", "--bare", remoteDir).Run(); err != nil {
		t.Fatal(err)
	}

	localDir := filepath.Join(t.TempDir(), name)
	if err := os.Mkdir(localDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initBulkGitRepo(t, localDir)
	cmd := exec.Command("git", "remote", "add", "origin", remoteDir)
	cmd.Dir = localDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := actions.ActionCommit(localDir, "initial"); err != nil {
		t.Fatal(err)
	}
	return remoteDir, &vcs.RepoStatus{Path: localDir, Type: vcs.Git, Remote: true, Ahead: vcs.Count{N: 1, Known: true}}
}

func initBulkGitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.name", "Test User"},
		{"config", "user.email", "test@example.com"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
}

func remoteSeesCommit(t *testing.T, remoteDir string) bool {
	t.Helper()
	cmd := exec.Command("git", "log", "--pretty=%s")
	cmd.Dir = remoteDir
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), "initial")
}

func TestRunBulkPushDryRunActsNot(t *testing.T) {
	remoteDir, status := pushFixture(t, "repo-a")

	var stdout, stderr bytes.Buffer
	code := runBulk(actions.BulkPush, []*vcs.RepoStatus{status}, true, false, nonTTYStdin(t), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Will push 1 repository:") {
		t.Fatalf("expected the plan, got %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "pushed") {
		t.Fatalf("a dry run must not report results, got %q", stdout.String())
	}
	if remoteSeesCommit(t, remoteDir) {
		t.Fatal("dry run must not push")
	}
}

func TestRunBulkPushYesEndToEnd(t *testing.T) {
	remoteA, statusA := pushFixture(t, "repo-a")
	remoteB, statusB := pushFixture(t, "repo-b")

	var stdout, stderr bytes.Buffer
	code := runBulk(actions.BulkPush, []*vcs.RepoStatus{statusA, statusB}, false, true, nonTTYStdin(t), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"Will push 2 repositories:", "✓ repo-a", "✓ repo-b", "2 pushed, 0 failed"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if !remoteSeesCommit(t, remoteA) || !remoteSeesCommit(t, remoteB) {
		t.Fatal("push did not reach the bare remotes")
	}
}

func TestRunBulkPushFailureExits1(t *testing.T) {
	remoteA, statusA := pushFixture(t, "repo-a")
	remoteB, statusB := pushFixture(t, "repo-b")
	if err := os.RemoveAll(remoteB); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runBulk(actions.BulkPush, []*vcs.RepoStatus{statusA, statusB}, false, true, nonTTYStdin(t), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "✓ repo-a") || !strings.Contains(out, "✗ repo-b") {
		t.Fatalf("expected one success and one failure line:\n%s", out)
	}
	if !strings.Contains(out, "1 pushed, 1 failed") || !strings.Contains(out, "repo-b") {
		t.Fatalf("expected a summary naming the failure:\n%s", out)
	}
	if !remoteSeesCommit(t, remoteA) {
		t.Fatal("the healthy repository did not push when its sibling failed")
	}
}

func TestRunBulkCommitPreflightMissingTool(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var stdout, stderr bytes.Buffer
	results := []*vcs.RepoStatus{
		{Path: "/work/dirty", Type: vcs.Git, Dirty: true},
	}
	code := runBulk(actions.BulkCommit, results, false, true, nonTTYStdin(t), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "git-ai-commit") || !strings.Contains(stderr.String(), "dirty") {
		t.Fatalf("expected the refusal to name the tool and the repo, got %q", stderr.String())
	}
	if strings.Contains(stdout.String(), "committed") {
		t.Fatalf("nothing may run when preflight fails, got %q", stdout.String())
	}
}

func TestFailureLineDoesNotDoubleTheVerb(t *testing.T) {
	// Backend errors already read "push failed: ..."; the result line must
	// not prefix a second "push failed:".
	got := failureLine(actions.BulkPush, errors.New("push failed: exit status 128"))
	if strings.Count(got, "push failed") != 1 {
		t.Fatalf("verb doubled in %q", got)
	}
	// An error that does not already name the operation still gets one.
	got = failureLine(actions.BulkPush, errors.New("no origin remote configured"))
	if !strings.HasPrefix(got, "push failed: ") {
		t.Fatalf("expected the verb to be added, got %q", got)
	}
}

func TestIndentedPreservesToolOutput(t *testing.T) {
	// The AI commit tools format for humans, so their dry-run output is
	// reproduced verbatim rather than scraped for a single message line.
	out := indented("=== Changes ===\nchore: add note.txt\n")
	if !strings.Contains(out, "\n    === Changes ===") {
		t.Fatalf("expected the header preserved and indented, got %q", out)
	}
	if !strings.Contains(out, "\n    chore: add note.txt") {
		t.Fatalf("expected the message preserved and indented, got %q", out)
	}
}
