package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const defaultCommitMessage = "Update via gitsync"

// ActionPush pushes the current repository.
func ActionPush(path string) error {
	if isJujutsu(path) {
		cmd := exec.Command("jj", "git", "push", "--all")
		cmd.Dir = path
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("push failed: %w\n%s", err, output)
		}
		return nil
	}

	// Git: try simple push first; if no upstream, set it up.
	cmd := exec.Command("git", "push")
	cmd.Dir = path
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if strings.Contains(string(output), "no upstream branch") {
		cmd = exec.Command("git", "push", "-u", "origin", "HEAD")
		cmd.Dir = path
		output, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("push failed: %w\n%s", err, output)
		}
		return nil
	}
	return fmt.Errorf("push failed: %w\n%s", err, output)
}

// ActionPull pulls the current repository.
func ActionPull(path string) error {
	if isJujutsu(path) {
		cmd := exec.Command("jj", "git", "fetch")
		cmd.Dir = path
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("fetch failed: %w\n%s", err, output)
		}
		return nil
	}

	cmd := exec.Command("git", "pull", "--rebase")
	cmd.Dir = path
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pull failed: %w\n%s", err, output)
	}
	return nil
}

// ActionCommit commits all current changes with the supplied message.
func ActionCommit(path, message string) error {
	if message == "" {
		message = defaultCommitMessage
	}
	if isJujutsu(path) {
		cmd := exec.Command("jj", "commit", "-m", message)
		cmd.Dir = path
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("commit failed: %w\n%s", err, output)
		}
		return nil
	}

	// Stage all changes (including untracked) and commit.
	add := exec.Command("git", "add", "-A")
	add.Dir = path
	if output, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("git add failed: %w\n%s", err, output)
	}
	cmd := exec.Command("git", "commit", "-m", message)
	cmd.Dir = path
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Nothing to commit is not a failure.
		if strings.Contains(string(output), "nothing to commit") {
			return nil
		}
		return fmt.Errorf("commit failed: %w\n%s", err, output)
	}
	return nil
}

// ActionSync pulls then pushes the current repository.
func ActionSync(path string) error {
	if err := ActionPull(path); err != nil {
		return fmt.Errorf("sync pull: %w", err)
	}
	if err := ActionPush(path); err != nil {
		return fmt.Errorf("sync push: %w", err)
	}
	return nil
}

// ActionRepair attempts to recover a corrupted Git repository.
// It returns whether repair succeeded, a human-readable message, and any error.
func ActionRepair(path string) (bool, string, error) {
	if isJujutsu(path) {
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
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return fmt.Errorf("GITHUB_TOKEN environment variable is required")
	}

	client := NewGitHubClient(token)
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
	if hasRemote(path, "origin") {
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

	// Try to set up tracking for the current branch.
	_ = setupTracking(path, "origin")

	return nil
}

func isJujutsu(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".jj"))
	return err == nil
}

func getOriginURL(path string) (string, error) {
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	cmd.Dir = path
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func hasRemote(path, name string) bool {
	cmd := exec.Command("git", "remote")
	cmd.Dir = path
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}
	return false
}

func currentBranch(path string) string {
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = path
	output, err := cmd.Output()
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
	cmd := exec.Command("git", "branch", "-u", remote+"/"+branch)
	cmd.Dir = path
	output, err := cmd.CombinedOutput()
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
	init := exec.Command("git", "init")
	init.Dir = path
	if output, err := init.CombinedOutput(); err != nil {
		_ = os.Rename(backupDir, gitDir)
		return false, fmt.Sprintf("re-init failed: %v\n%s", err, output)
	}

	add := exec.Command("git", "remote", "add", "origin", url)
	add.Dir = path
	if output, err := add.CombinedOutput(); err != nil {
		_ = os.Rename(backupDir, gitDir)
		return false, fmt.Sprintf("remote add failed: %v\n%s", err, output)
	}

	fetch := exec.Command("git", "fetch", "origin")
	fetch.Dir = path
	if output, err := fetch.CombinedOutput(); err != nil {
		_ = os.Rename(backupDir, gitDir)
		return false, fmt.Sprintf("fetch failed: %v\n%s", err, output)
	}

	// Try to checkout the default branch.
	branch := "main"
	for _, candidate := range []string{"main", "master"} {
		cmd := exec.Command("git", "show-ref", "--verify", "refs/remotes/origin/"+candidate)
		cmd.Dir = path
		if cmd.Run() == nil {
			branch = candidate
			break
		}
	}

	// Commit current working-tree files before switching branches so they are not lost.
	_ = exec.Command("git", "add", "-A").Run()
	_ = exec.Command("git", "commit", "-m", "Save local changes before repair").Run()

	checkout := exec.Command("git", "checkout", "-B", branch, "origin/"+branch)
	checkout.Dir = path
	if output, err := checkout.CombinedOutput(); err != nil {
		_ = os.Rename(backupDir, gitDir)
		return false, fmt.Sprintf("checkout failed: %v\n%s", err, output)
	}

	_ = os.RemoveAll(backupDir)
	return true, fmt.Sprintf("Recovered from remote %s on branch %s", url, branch)
}

func attemptLocalRepair(path string) (bool, string) {
	cmds := [][]string{
		{"git", "fsck", "--full"},
		{"git", "update-ref", "HEAD", "HEAD"},
		{"git", "fetch", "--all"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = path
		if output, err := cmd.CombinedOutput(); err != nil {
			return false, fmt.Sprintf("local repair step %q failed: %v\n%s", args, err, output)
		}
	}
	return true, "Local repair completed"
}
