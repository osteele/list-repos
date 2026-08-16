package main

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
	client := NewGitHubClient("test-token")
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

func actionAddGitHubRemoteWithClient(path string, client *GitHubClient) error {
	username, err := client.GetUsername()
	if err != nil {
		return err
	}
	repoName := filepath.Base(path)
	cloneURL, err := client.FindRepo(repoName, username)
	if err != nil {
		return err
	}
	if hasOrigin(path) {
		cmd := exec.Command("git", "remote", "remove", "origin")
		cmd.Dir = path
		_ = cmd.Run()
	}
	cmd := exec.Command("git", "remote", "add", "origin", cloneURL)
	cmd.Dir = path
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to add origin: %w\n%s", err, output)
	}
	_ = setupTracking(path, "origin")
	return nil
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
