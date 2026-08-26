package actions

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/osteele/gitsync/internal/vcs"
)

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
}

func TestActionCommit(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "action-commit")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	initGitRepo(t, tmpDir)
	if err := os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ActionCommit(tmpDir, "test commit"); err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	cmd := exec.Command("git", "log", "-1", "--pretty=%s")
	cmd.Dir = tmpDir
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "test commit\n" {
		t.Fatalf("unexpected commit message: %q", output)
	}
}

func TestActionPush(t *testing.T) {
	// Isolate from global git config: a global push.autoSetupRemote=true
	// makes even a plain push configure the upstream, which would mask the
	// explicit upstream setup under test. (A repo-local autoSetupRemote=false
	// does not reliably override the global setting.)
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)

	// Create a bare remote and a local repo with a commit to push.
	remoteDir, err := os.MkdirTemp("", "action-push-remote")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(remoteDir) }()
	cmd := exec.Command("git", "init", "--bare")
	cmd.Dir = remoteDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	localDir, err := os.MkdirTemp("", "action-push-local")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(localDir) }()
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

	if err := ActionPush(localDir); err != nil {
		t.Fatalf("push failed: %v", err)
	}

	// The branch had no upstream before the push; pushing must have set one.
	cmd = exec.Command("git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	cmd.Dir = localDir
	upstream, err := cmd.Output()
	if err != nil {
		t.Fatalf("expected upstream to be configured after push: %v", err)
	}
	if !strings.HasPrefix(string(upstream), "origin/") {
		t.Fatalf("expected upstream on origin, got %q", upstream)
	}

	cmd = exec.Command("git", "log", "--pretty=%s")
	cmd.Dir = remoteDir
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "initial\n" {
		t.Fatalf("remote does not have commit: %q", output)
	}
}

func TestActionPushNoRemote(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "action-push-noremote")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	initGitRepo(t, tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ActionCommit(tmpDir, "initial"); err != nil {
		t.Fatal(err)
	}

	err = ActionPush(tmpDir)
	if err == nil {
		t.Fatal("expected push to fail with no remote")
	}
	if !strings.Contains(err.Error(), "no origin remote") {
		t.Fatalf("expected no origin remote error, got: %v", err)
	}
}

func TestActionCommitNothing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "action-commit-nothing")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	initGitRepo(t, tmpDir)

	if err := ActionCommit(tmpDir, "empty commit"); err != nil {
		t.Fatalf("commit with no changes should not error: %v", err)
	}
}

func TestActionPull(t *testing.T) {
	// Create a bare remote with a commit, clone it, add another commit to remote, then pull.
	remoteDir, err := os.MkdirTemp("", "action-pull-remote")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(remoteDir) }()
	cmd := exec.Command("git", "init", "--bare")
	cmd.Dir = remoteDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	localDir, err := os.MkdirTemp("", "action-pull-local")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(localDir) }()
	initGitRepo(t, localDir)
	cmd = exec.Command("git", "remote", "add", "origin", remoteDir)
	cmd.Dir = localDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ActionCommit(localDir, "first"); err != nil {
		t.Fatal(err)
	}
	if err := ActionPush(localDir); err != nil {
		t.Fatal(err)
	}

	// Add another commit to remote by pushing from a second clone.
	cloneDir, err := os.MkdirTemp("", "action-pull-clone")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(cloneDir) }()
	cmd = exec.Command("git", "clone", remoteDir, cloneDir)
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, cloneDir) // re-set user config after clone
	if err := os.WriteFile(filepath.Join(cloneDir, "other.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ActionCommit(cloneDir, "second"); err != nil {
		t.Fatal(err)
	}
	if err := ActionPush(cloneDir); err != nil {
		t.Fatal(err)
	}

	// Pull into original local repo.
	if err := ActionPull(localDir); err != nil {
		t.Fatalf("pull failed: %v", err)
	}

	cmd = exec.Command("git", "log", "--pretty=%s")
	cmd.Dir = localDir
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), "second") {
		t.Fatalf("local repo missing pulled commit: %q", output)
	}
}

func TestActionRepairLocal(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "action-repair")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	initGitRepo(t, tmpDir)
	if err := os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ActionCommit(tmpDir, "initial"); err != nil {
		t.Fatal(err)
	}

	ok, msg, err := ActionRepair(tmpDir)
	if err != nil {
		t.Fatalf("repair error: %v", err)
	}
	if !ok {
		t.Fatalf("repair failed: %s", msg)
	}
}

// setupRecoverableRepo builds a local repo whose origin has a main branch,
// with an uncommitted edit to a tracked file. The edit makes the repair's
// branch checkout fail unless the local changes are committed first.
func setupRecoverableRepo(t *testing.T) (remoteDir, localDir string) {
	t.Helper()
	remoteDir, err := os.MkdirTemp("", "recover-remote")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(remoteDir) })
	cmd := exec.Command("git", "init", "--bare")
	cmd.Dir = remoteDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	localDir, err = os.MkdirTemp("", "recover-local")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(localDir) })
	initGitRepo(t, localDir)
	if err := os.WriteFile(filepath.Join(localDir, "file.txt"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ActionCommit(localDir, "initial"); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "branch", "-M", "main")
	cmd.Dir = localDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "remote", "add", "origin", remoteDir)
	cmd.Dir = localDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "push", "-u", "origin", "main")
	cmd.Dir = localDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "file.txt"), []byte("local edits"), 0o644); err != nil {
		t.Fatal(err)
	}
	return remoteDir, localDir
}

// chdirScratch runs the test from a scratch directory so that commands which
// (incorrectly) use the process CWD cannot touch the real repository.
func chdirScratch(t *testing.T) {
	t.Helper()
	scratch, err := os.MkdirTemp("", "recover-cwd")
	if err != nil {
		t.Fatal(err)
	}
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(scratch); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWd)
		_ = os.RemoveAll(scratch)
	})
}

func TestRecoverFromRemotePreservesLocalEdits(t *testing.T) {
	remoteDir, localDir := setupRecoverableRepo(t)
	chdirScratch(t)

	ok, msg := recoverFromRemote(localDir, remoteDir)
	if !ok {
		t.Fatalf("recover failed: %s", msg)
	}

	// The local edits must survive the repair as a safety commit in the repo.
	cmd := exec.Command("git", "reflog")
	cmd.Dir = localDir
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "Save local changes before repair") {
		t.Fatalf("safety commit missing from repaired repo reflog: %q", output)
	}
}

func TestRecoverFromRemoteKeepsBackup(t *testing.T) {
	remoteDir, localDir := setupRecoverableRepo(t)
	chdirScratch(t)

	ok, msg := recoverFromRemote(localDir, remoteDir)
	if !ok {
		t.Fatalf("recover failed: %s", msg)
	}
	backupDir := filepath.Join(localDir, ".git.broken")
	if _, err := os.Stat(backupDir); err != nil {
		t.Fatalf("expected backup %s to survive repair: %v", backupDir, err)
	}
	if !strings.Contains(msg, backupDir) {
		t.Fatalf("expected success message to mention backup location, got %q", msg)
	}
}

func TestActionAddGitHubRemote(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "action-ghremote")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	initGitRepo(t, tmpDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/user":
			_ = json.NewEncoder(w).Encode(map[string]string{"login": "testuser"})
		case strings.HasPrefix(r.URL.Path, "/repos/testuser/"):
			repoName := strings.TrimPrefix(r.URL.Path, "/repos/testuser/")
			_ = json.NewEncoder(w).Encode(map[string]string{"clone_url": "https://github.com/testuser/" + repoName + ".git"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// Inject test server URL through the client by temporarily swapping the global default.
	client := vcs.NewGitHubClient("test-token")
	client.BaseURL = server.URL

	if err := actionAddGitHubRemoteWithClient(tmpDir, client); err != nil {
		t.Fatalf("add github remote failed: %v", err)
	}

	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = tmpDir
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	expected := "https://github.com/testuser/" + filepath.Base(tmpDir) + ".git\n"
	if string(output) != expected {
		t.Fatalf("expected %q, got %q", expected, output)
	}
}

func TestDescribeChangesUsesReadOnlyToolMode(t *testing.T) {
	for _, tt := range []struct {
		name     string
		metadata string
		tool     string
	}{
		{name: "git", metadata: ".git", tool: "git-ai-commit"},
		{name: "jj", metadata: ".jj", tool: "jj-ai-commit"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			if err := os.Mkdir(filepath.Join(repo, tt.metadata), 0o755); err != nil {
				t.Fatal(err)
			}
			binDir := t.TempDir()
			toolPath := filepath.Join(binDir, tt.tool)
			payload := fmt.Sprintf(`{"schemaVersion":"ai-describe/v1","tool":{"name":%q},"model":{"requested":"auto","display":"mock/model"},"results":[{"target":{"kind":"working-copy","display":"working copy"},"previousDescription":"","description":"fix: describe changes","files":[{"status":"modified","path":"README.md"}],"diffBytes":123,"applied":false}]}`, tt.tool)
			script := "#!/bin/sh\n[ \"$*\" = \"--dry-run --json\" ] || exit 9\nprintf '%s' '" + payload + "'\n"
			if err := os.WriteFile(toolPath, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

			got, err := DescribeChanges(repo)
			if err != nil {
				t.Fatal(err)
			}
			if got.Tool != tt.tool || got.ModelDisplay != "mock/model" || got.CommitMessage != "fix: describe changes" {
				t.Fatalf("DescribeChanges() = %+v", got)
			}
			if len(got.Files) != 1 || got.Files[0].Path != "README.md" || got.DiffBytes != 123 {
				t.Fatalf("DescribeChanges() lost structured data: %+v", got)
			}
		})
	}
}

func TestDecodeChangeDescriptionValidatesContract(t *testing.T) {
	payload := []byte(`{
		"schemaVersion": "ai-describe/v1",
		"tool": {"name": "jj-ai-commit"},
		"model": {"requested": "auto", "display": "mock/model"},
		"results": [{
			"target": {"kind": "working-copy", "display": "@", "id": "abc"},
			"previousDescription": "",
			"description": "feat: add the application shell\n\nExplain routing.",
			"files": [{"status": "added", "path": "README.md"}],
			"diffBytes": 456,
			"applied": false
		}]
	}`)
	got, err := decodeChangeDescription(payload)
	if err != nil {
		t.Fatal(err)
	}
	if got.CommitMessage != "feat: add the application shell\n\nExplain routing." || got.Target.ID != "abc" {
		t.Fatalf("decodeChangeDescription() = %+v", got)
	}
}

func TestDecodeChangeDescriptionRejectsInvalidContracts(t *testing.T) {
	tests := map[string]string{
		"wrong version":   `{"schemaVersion":"ai-describe/v2"}`,
		"missing result":  `{"schemaVersion":"ai-describe/v1","tool":{"name":"x"},"model":{"display":"m"},"results":[]}`,
		"applied result":  `{"schemaVersion":"ai-describe/v1","tool":{"name":"x"},"model":{"display":"m"},"results":[{"target":{"kind":"working-copy","display":"@"},"description":"fix: x","files":[],"diffBytes":1,"applied":true}]}`,
		"missing applied": `{"schemaVersion":"ai-describe/v1","tool":{"name":"x"},"model":{"display":"m"},"results":[{"target":{"kind":"working-copy","display":"@"},"description":"fix: x","files":[],"diffBytes":1}]}`,
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeChangeDescription([]byte(payload)); err == nil {
				t.Fatal("expected invalid contract to fail")
			}
		})
	}
}

func TestDescriptionInputHashTracksGitChanges(t *testing.T) {
	repo := t.TempDir()
	initGitRepo(t, repo)
	readme := filepath.Join(repo, "README.md")
	if err := os.WriteFile(readme, []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "README.md"}, {"commit", "-m", "initial"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
		}
	}
	if err := os.WriteFile(readme, []byte("first change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := DescriptionInputHash(repo)
	if err != nil {
		t.Fatal(err)
	}
	again, err := DescriptionInputHash(repo)
	if err != nil {
		t.Fatal(err)
	}
	if first != again {
		t.Fatalf("unchanged diff hash changed: %q != %q", first, again)
	}
	if err := os.WriteFile(readme, []byte("second change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := DescriptionInputHash(repo)
	if err != nil {
		t.Fatal(err)
	}
	if changed == first {
		t.Fatal("changed Git diff retained its description hash")
	}

	untracked := filepath.Join(repo, "notes.txt")
	if err := os.WriteFile(untracked, []byte("first untracked contents\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	untrackedFirst, err := DescriptionInputHash(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(untracked, []byte("second untracked contents\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	untrackedChanged, err := DescriptionInputHash(repo)
	if err != nil {
		t.Fatal(err)
	}
	if untrackedChanged == untrackedFirst {
		t.Fatal("changed untracked contents retained their description hash")
	}
}

func TestDescriptionInputHashTracksJujutsuChanges(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj is not installed")
	}
	repo := t.TempDir()
	cmd := exec.Command("jj", "git", "init")
	cmd.Dir = repo
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("jj git init: %v\n%s", err, output)
	}
	readme := filepath.Join(repo, "README.md")
	if err := os.WriteFile(readme, []byte("first change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := DescriptionInputHash(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(readme, []byte("second change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := DescriptionInputHash(repo)
	if err != nil {
		t.Fatal(err)
	}
	if changed == first {
		t.Fatal("changed Jujutsu diff retained its description hash")
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
