package actions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/osteele/gitsync/internal/vcs"
)

// DefaultCommitMessage is used when no commit message is supplied.
const DefaultCommitMessage = "Update via gitsync"

// ActionPush pushes the current repository.
func ActionPush(path string) error {
	return vcs.BackendFor(vcs.DetectRepoType(path)).Push(path)
}

// ActionPull pulls the current repository.
func ActionPull(path string) error {
	return vcs.BackendFor(vcs.DetectRepoType(path)).Pull(path)
}

// ActionCommit commits all current changes with the supplied message.
func ActionCommit(path, message string) error {
	if message == "" {
		message = DefaultCommitMessage
	}
	return vcs.BackendFor(vcs.DetectRepoType(path)).Commit(path, message)
}

// ActionSync pulls then pushes the current repository.
func ActionSync(path string) error {
	backend := vcs.BackendFor(vcs.DetectRepoType(path))
	if err := backend.Pull(path); err != nil {
		return fmt.Errorf("sync pull: %w", err)
	}
	if err := backend.Push(path); err != nil {
		return fmt.Errorf("sync push: %w", err)
	}
	return nil
}

// ActionRepair attempts to recover a corrupted Git repository.
// It returns whether repair succeeded, a human-readable message, and any error.
func ActionRepair(path string) (bool, string, error) {
	if vcs.DetectRepoType(path) != vcs.Git {
		return false, "", fmt.Errorf("repair is only supported for Git repositories")
	}

	// Try remote recovery first if we can read an origin URL.
	if url, err := getOriginURL(path); err == nil && url != "" {
		if ok, msg := recoverFromRemote(path, url); ok {
			return true, msg, nil
		}
	}

	// Fall back to local repair.
	if ok, msg := attemptLocalRepair(path); ok {
		return true, msg, nil
	}

	return false, "Repository could not be repaired automatically.", nil
}

// ActionAddGitHubRemote finds the user's GitHub repo matching the directory name
// and sets it as origin, creating or replacing the remote as needed.
func ActionAddGitHubRemote(path string) error {
	if vcs.DetectRepoType(path) != vcs.Git {
		return fmt.Errorf("GitHub remote setup is only supported for Git repositories")
	}

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return fmt.Errorf("GITHUB_TOKEN environment variable is required")
	}

	client := vcs.NewGitHubClient(token)
	return actionAddGitHubRemoteWithClient(path, client)
}

// actionAddGitHubRemoteWithClient is ActionAddGitHubRemote with an injectable
// client, so tests can point at a stub GitHub API server.
func actionAddGitHubRemoteWithClient(path string, client *vcs.GitHubClient) error {
	username, err := client.GetUsername()
	if err != nil {
		return fmt.Errorf("could not determine GitHub user: %w", err)
	}

	repoName := filepath.Base(path)
	cloneURL, err := client.FindRepo(repoName, username)
	if err != nil {
		return fmt.Errorf("could not find GitHub repository %q: %w", repoName, err)
	}

	// Remove existing origin if present.
	if vcs.HasOrigin(path) {
		_, _ = vcs.RunVCS(path, vcs.StatusTimeout, "git", "remote", "remove", "origin")
	}

	output, err := vcs.RunVCS(path, vcs.NetworkTimeout, "git", "remote", "add", "origin", cloneURL)
	if err != nil {
		return fmt.Errorf("failed to add origin: %w\n%s", err, output)
	}

	// Try to set up tracking for the current branch.
	_ = setupTracking(path, "origin")

	return nil
}

func getOriginURL(path string) (string, error) {
	output, err := vcs.RunVCSOutput(path, "git", "config", "--get", "remote.origin.url")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func currentBranch(path string) string {
	output, err := vcs.RunVCSOutput(path, "git", "branch", "--show-current")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func setupTracking(path, remote string) error {
	branch := currentBranch(path)
	if branch == "" {
		return fmt.Errorf("no current branch")
	}
	output, err := vcs.RunVCS(path, vcs.StatusTimeout, "git", "branch", "-u", remote+"/"+branch)
	if err != nil {
		return fmt.Errorf("failed to set upstream: %w\n%s", err, output)
	}
	return nil
}

func recoverFromRemote(path, url string) (bool, string) {
	gitDir := filepath.Join(path, ".git")
	backupDir := gitDir + ".broken"

	// Move damaged .git aside.
	if err := os.Rename(gitDir, backupDir); err != nil {
		return false, ""
	}

	// Re-init and re-add remote.
	if output, err := vcs.RunVCS(path, vcs.StatusTimeout, "git", "init"); err != nil {
		_ = os.Rename(backupDir, gitDir)
		return false, fmt.Sprintf("re-init failed: %v\n%s", err, output)
	}

	if output, err := vcs.RunVCS(path, vcs.StatusTimeout, "git", "remote", "add", "origin", url); err != nil {
		_ = os.Rename(backupDir, gitDir)
		return false, fmt.Sprintf("remote add failed: %v\n%s", err, output)
	}

	if output, err := vcs.RunVCS(path, vcs.NetworkTimeout, "git", "fetch", "origin"); err != nil {
		_ = os.Rename(backupDir, gitDir)
		return false, fmt.Sprintf("fetch failed: %v\n%s", err, output)
	}

	// Try to checkout the default branch.
	branch := "main"
	for _, candidate := range []string{"main", "master"} {
		if _, err := vcs.RunVCS(path, vcs.StatusTimeout, "git", "show-ref", "--verify", "refs/remotes/origin/"+candidate); err == nil {
			branch = candidate
			break
		}
	}

	// Commit current working-tree files before switching branches so they are not lost.
	_, _ = vcs.RunVCS(path, vcs.StatusTimeout, "git", "config", "user.email", "gitsync@localhost")
	_, _ = vcs.RunVCS(path, vcs.StatusTimeout, "git", "config", "user.name", "gitsync")
	_, _ = vcs.RunVCS(path, vcs.StatusTimeout, "git", "add", "-A")
	_, _ = vcs.RunVCS(path, vcs.StatusTimeout, "git", "commit", "-m", "Save local changes before repair")

	if output, err := vcs.RunVCS(path, vcs.StatusTimeout, "git", "checkout", "-B", branch, "origin/"+branch); err != nil {
		_ = os.Rename(backupDir, gitDir)
		return false, fmt.Sprintf("checkout failed: %v\n%s", err, output)
	}

	// Keep the backup: it may hold stashes, reflog entries, and unpushed branches.
	return true, fmt.Sprintf("Recovered from remote %s on branch %s (old .git saved as %s)", url, branch, backupDir)
}

type repairStep struct {
	timeout time.Duration
	args    []string
}

func attemptLocalRepair(path string) (bool, string) {
	steps := []repairStep{
		{vcs.StatusTimeout, []string{"fsck", "--full"}},
		{vcs.StatusTimeout, []string{"update-ref", "HEAD", "HEAD"}},
	}
	if vcs.HasOrigin(path) {
		steps = append(steps, repairStep{vcs.NetworkTimeout, []string{"fetch", "origin"}})
	}
	for _, step := range steps {
		if output, err := vcs.RunVCS(path, step.timeout, "git", step.args...); err != nil {
			return false, fmt.Sprintf("local repair step %q failed: %v\n%s", step.args, err, output)
		}
	}
	return true, "Local repair completed"
}
