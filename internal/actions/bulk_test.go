package actions

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/osteele/gitsync/internal/vcs"
)

func TestEligible(t *testing.T) {
	count := func(n int) vcs.Count { return vcs.Count{N: n, Known: true} }
	unknown := vcs.Count{}

	cases := []struct {
		name string
		op   BulkOp
		s    *vcs.RepoStatus
		want bool
	}{
		// Non-repositories are never eligible.
		{"commit skips dir", BulkCommit, &vcs.RepoStatus{Type: vcs.Dir, Dirty: true}, false},
		{"push skips dir", BulkPush, &vcs.RepoStatus{Type: vcs.Dir, Remote: true, Ahead: count(2)}, false},

		{"commit dirty", BulkCommit, &vcs.RepoStatus{Type: vcs.Git, Dirty: true}, true},
		{"commit clean", BulkCommit, &vcs.RepoStatus{Type: vcs.Git}, false},

		{"pull behind", BulkPull, &vcs.RepoStatus{Type: vcs.Git, Remote: true, Behind: count(2)}, true},
		{"pull behind unknown", BulkPull, &vcs.RepoStatus{Type: vcs.Git, Remote: true, Behind: unknown}, true},
		{"pull up to date", BulkPull, &vcs.RepoStatus{Type: vcs.Git, Remote: true, Behind: count(0)}, false},
		{"pull no remote", BulkPull, &vcs.RepoStatus{Type: vcs.Git, Behind: count(2)}, false},

		{"push ahead", BulkPush, &vcs.RepoStatus{Type: vcs.Git, Remote: true, Ahead: count(1)}, true},
		{"push ahead unknown", BulkPush, &vcs.RepoStatus{Type: vcs.Git, Remote: true, Ahead: unknown}, false},
		{"push up to date", BulkPush, &vcs.RepoStatus{Type: vcs.Git, Remote: true, Ahead: count(0)}, false},
		{"push no remote", BulkPush, &vcs.RepoStatus{Type: vcs.Git, Ahead: count(1)}, false},

		{"sync pulls", BulkSync, &vcs.RepoStatus{Type: vcs.Git, Remote: true, Behind: count(1)}, true},
		{"sync pushes", BulkSync, &vcs.RepoStatus{Type: vcs.Git, Remote: true, Ahead: count(1), Behind: count(0)}, true},
		{"sync up to date", BulkSync, &vcs.RepoStatus{Type: vcs.Git, Remote: true, Ahead: count(0), Behind: count(0)}, false},
		{"sync no remote", BulkSync, &vcs.RepoStatus{Type: vcs.Git, Ahead: count(1), Behind: count(1)}, false},
	}
	for _, tc := range cases {
		if got := Eligible(tc.op, tc.s); got != tc.want {
			t.Errorf("%s: Eligible(%v, %+v) = %v, want %v", tc.name, tc.op, tc.s, got, tc.want)
		}
	}
}

func TestPlan(t *testing.T) {
	statuses := []*vcs.RepoStatus{
		{Path: "/work/dirty-one", Type: vcs.Git, Dirty: true},
		{Path: "/work/clean", Type: vcs.Git, Remote: true, Ahead: vcs.Count{N: 0, Known: true}, Behind: vcs.Count{N: 0, Known: true}},
		{Path: "/work/group", Type: vcs.Dir, Dirty: true},
		{Path: "/work/dirty-two", Type: vcs.Jujutsu, Dirty: true},
	}
	items := Plan(BulkCommit, statuses)
	if len(items) != 2 {
		t.Fatalf("expected 2 plan items, got %d", len(items))
	}
	if items[0].Status.Path != "/work/dirty-one" || items[1].Status.Path != "/work/dirty-two" {
		t.Fatalf("plan lost the listing order: %v", items)
	}
	if items[0].Reason != "dirty" {
		t.Fatalf("unexpected commit reason %q", items[0].Reason)
	}

	push := Plan(BulkPush, []*vcs.RepoStatus{
		{Path: "/work/a", Type: vcs.Git, Remote: true, Ahead: vcs.Count{N: 2, Known: true}},
	})
	if len(push) != 1 || push[0].Reason != "ahead 2" {
		t.Fatalf("unexpected push plan: %+v", push)
	}

	pull := Plan(BulkPull, []*vcs.RepoStatus{
		{Path: "/work/b", Type: vcs.Git, Remote: true, Behind: vcs.Count{}},
	})
	if len(pull) != 1 || pull[0].Reason != "behind ?" {
		t.Fatalf("unexpected pull plan: %+v", pull)
	}

	sync := Plan(BulkSync, []*vcs.RepoStatus{
		{Path: "/work/c", Type: vcs.Git, Remote: true, Ahead: vcs.Count{N: 1, Known: true}, Behind: vcs.Count{N: 3, Known: true}},
	})
	if len(sync) != 1 || sync[0].Reason != "ahead 1, behind 3" {
		t.Fatalf("unexpected sync plan: %+v", sync)
	}
}

func TestRenderPlan(t *testing.T) {
	var buf bytes.Buffer
	RenderPlan(&buf, BulkPush, []PlanItem{
		{Status: &vcs.RepoStatus{Path: "/work/agent-mail"}, Reason: "ahead 1"},
		{Status: &vcs.RepoStatus{Path: "/work/claude-wrapper"}, Reason: "ahead 2"},
	})
	out := buf.String()
	for _, want := range []string{"Will push 2 repositories:", "agent-mail", "ahead 1", "claude-wrapper", "ahead 2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("plan output missing %q:\n%s", want, out)
		}
	}

	buf.Reset()
	RenderPlan(&buf, BulkPull, []PlanItem{{Status: &vcs.RepoStatus{Path: "/work/one"}, Reason: "behind ?"}})
	if !strings.Contains(buf.String(), "Will pull 1 repository:") {
		t.Fatalf("singular plan header wrong:\n%s", buf.String())
	}

	if got := EmptyMessage(BulkPush); got != "Nothing to push." {
		t.Fatalf("unexpected empty message %q", got)
	}
}

// bulkPushFixture builds a local repo with one unpushed commit against a
// bare remote on disk, and the plan item selecting it for a bulk push.
func bulkPushFixture(t *testing.T, name string) (remoteDir string, item PlanItem) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)

	remoteDir = filepath.Join(t.TempDir(), name+".git")
	cmd := exec.Command("git", "init", "--bare", remoteDir)
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	localDir := filepath.Join(t.TempDir(), name)
	if err := os.Mkdir(localDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, localDir)
	cmd = exec.Command("git", "remote", "add", "origin", remoteDir)
	cmd.Dir = localDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ActionCommit(localDir, "initial"); err != nil {
		t.Fatal(err)
	}

	status := &vcs.RepoStatus{Path: localDir, Type: vcs.Git, Remote: true, Ahead: vcs.Count{N: 1, Known: true}}
	return remoteDir, PlanItem{Status: status, Reason: "ahead 1"}
}

func remoteHasCommit(t *testing.T, remoteDir, message string) bool {
	t.Helper()
	cmd := exec.Command("git", "log", "--pretty=%s")
	cmd.Dir = remoteDir
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), message)
}

func TestExecuteBulkPushEndToEnd(t *testing.T) {
	remoteA, itemA := bulkPushFixture(t, "repo-a")
	remoteB, itemB := bulkPushFixture(t, "repo-b")

	var lines []string
	succeeded, failed := ExecuteBulk(BulkPush, []PlanItem{itemA, itemB}, false, func(r Result) {
		if r.Err != nil {
			lines = append(lines, "fail: "+r.Err.Error())
		} else {
			lines = append(lines, "ok: "+filepath.Base(r.Item.Status.Path))
		}
	})
	if failed != 0 || succeeded != 2 {
		t.Fatalf("expected 2 pushed, 0 failed; got %d/%d: %v", succeeded, failed, lines)
	}
	if len(lines) != 2 {
		t.Fatalf("expected one result per repository, got %v", lines)
	}
	if !remoteHasCommit(t, remoteA, "initial") || !remoteHasCommit(t, remoteB, "initial") {
		t.Fatal("push did not reach the bare remotes")
	}
}

func TestExecuteBulkFailureIsolation(t *testing.T) {
	remoteA, itemA := bulkPushFixture(t, "repo-a")
	// repo-b's remote is deleted after the scan, so its push fails.
	remoteB, itemB := bulkPushFixture(t, "repo-b")
	if err := os.RemoveAll(remoteB); err != nil {
		t.Fatal(err)
	}

	succeeded, failed := ExecuteBulk(BulkPush, []PlanItem{itemA, itemB}, false, nil)
	if succeeded != 1 || failed != 1 {
		t.Fatalf("expected 1 pushed, 1 failed; got %d/%d", succeeded, failed)
	}
	if !remoteHasCommit(t, remoteA, "initial") {
		t.Fatal("the healthy repository did not push when its sibling failed")
	}
}

func TestExecuteBulkPullEndToEnd(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	remoteDir, item := bulkPushFixture(t, "repo-a")
	localDir := item.Status.Path

	// Push the commit, then add a second commit from another clone so the
	// local repo is behind.
	if _, err := runOne(BulkPush, localDir, false); err != nil {
		t.Fatal(err)
	}
	cloneDir := filepath.Join(t.TempDir(), "clone")
	cmd := exec.Command("git", "clone", remoteDir, cloneDir)
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, cloneDir)
	if err := os.WriteFile(filepath.Join(cloneDir, "other.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ActionCommit(cloneDir, "second"); err != nil {
		t.Fatal(err)
	}
	if err := ActionPush(cloneDir); err != nil {
		t.Fatal(err)
	}

	item.Reason = "behind 1"
	item.Status.Behind = vcs.Count{N: 1, Known: true}
	succeeded, failed := ExecuteBulk(BulkPull, []PlanItem{item}, false, nil)
	if failed != 0 || succeeded != 1 {
		t.Fatalf("expected 1 pulled, 0 failed; got %d/%d", succeeded, failed)
	}

	cmd = exec.Command("git", "log", "--pretty=%s")
	cmd.Dir = localDir
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "second") {
		t.Fatalf("pull did not bring the new commit: %q", output)
	}
}

// fakeCommitTool installs an executable named tool on PATH that appends its
// arguments to recordFile and runs body. It returns the record file's path.
func fakeCommitTool(t *testing.T, tool, body string) string {
	t.Helper()
	binDir := t.TempDir()
	recordFile := filepath.Join(t.TempDir(), "args")
	name := tool
	script := "#!/bin/sh\necho \"$@\" >> " + recordFile + "\n" + body + "\n"
	if runtime.GOOS == "windows" {
		name = tool + ".bat"
		script = "@echo off\r\necho %* >> \"" + recordFile + "\"\r\n" + body + "\r\n"
	}
	if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return recordFile
}

func TestPreflightCommitToolsMissing(t *testing.T) {
	// A PATH with neither AI commit tool must refuse, naming the tool and
	// the affected repositories.
	t.Setenv("PATH", t.TempDir())
	items := []PlanItem{
		{Status: &vcs.RepoStatus{Path: "/work/alpha", Type: vcs.Git, Dirty: true}, Reason: "dirty"},
		{Status: &vcs.RepoStatus{Path: "/work/beta", Type: vcs.Jujutsu, Dirty: true}, Reason: "dirty"},
	}
	err := PreflightCommitTools(items)
	if err == nil {
		t.Fatal("expected a preflight refusal")
	}
	for _, want := range []string{"git-ai-commit", "jj-ai-commit", "alpha", "beta"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("preflight error missing %q: %v", want, err)
		}
	}
}

func TestPreflightCommitToolsPresent(t *testing.T) {
	fakeCommitTool(t, "git-ai-commit", "exit 0")
	items := []PlanItem{
		{Status: &vcs.RepoStatus{Path: "/work/alpha", Type: vcs.Git, Dirty: true}, Reason: "dirty"},
	}
	if err := PreflightCommitTools(items); err != nil {
		t.Fatalf("expected preflight to pass, got %v", err)
	}
}

func TestExecuteBulkCommitUsesAITool(t *testing.T) {
	recordFile := fakeCommitTool(t, "git-ai-commit", "git add -A && git commit -q -m \"ai: generated message\"")

	localDir := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(localDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, localDir)
	if err := os.WriteFile(filepath.Join(localDir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	status := &vcs.RepoStatus{Path: localDir, Type: vcs.Git, Dirty: true}
	succeeded, failed := ExecuteBulk(BulkCommit, []PlanItem{{Status: status, Reason: "dirty"}}, false, nil)
	if failed != 0 || succeeded != 1 {
		t.Fatalf("expected 1 committed, 0 failed; got %d/%d", succeeded, failed)
	}

	cmd := exec.Command("git", "log", "-1", "--pretty=%s")
	cmd.Dir = localDir
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(output)) != "ai: generated message" {
		t.Fatalf("expected the AI tool's message, got %q", output)
	}

	recorded, err := os.ReadFile(recordFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(recorded), "--dry-run") {
		t.Fatalf("a real commit must not pass --dry-run, got %q", recorded)
	}
}

func TestExecuteBulkCommitDryRunRequestsMessageOnly(t *testing.T) {
	recordFile := fakeCommitTool(t, "git-ai-commit", "echo 'feat: would commit this'")

	localDir := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(localDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, localDir)
	if err := os.WriteFile(filepath.Join(localDir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	var output string
	status := &vcs.RepoStatus{Path: localDir, Type: vcs.Git, Dirty: true}
	succeeded, failed := ExecuteBulk(BulkCommit, []PlanItem{{Status: status, Reason: "dirty"}}, true, func(r Result) {
		output = r.Output
	})
	if failed != 0 || succeeded != 1 {
		t.Fatalf("expected 1 dry-run success, got %d/%d", succeeded, failed)
	}
	if !strings.Contains(output, "feat: would commit this") {
		t.Fatalf("expected the tool's dry-run output, got %q", output)
	}

	recorded, err := os.ReadFile(recordFile)
	if err != nil {
		t.Fatal(err)
	}
	// The draft is requested with --print-message, which implies dry-run and
	// returns the message alone, rather than --dry-run whose human-facing
	// report would have to be scraped.
	if !strings.Contains(string(recorded), "--print-message") {
		t.Fatalf("expected --print-message to reach the tool, got %q", recorded)
	}

	// A dry run commits nothing: the repo is still dirty.
	if err := vcs.BackendFor(vcs.Git).Status(&vcs.RepoStatus{Path: localDir}); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = localDir
	porcelain, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(porcelain) == 0 {
		t.Fatal("dry run must leave the repository dirty")
	}
}
