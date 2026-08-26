package actions

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/osteele/gitsync/internal/vcs"
)

// DefaultCommitMessage is used when no commit message is supplied.
const DefaultCommitMessage = "Update via gitsync"

const descriptionSchemaVersion = "ai-describe/v1"

// DescriptionToolFor names the optional AI description tool for a repository
// type. The tools own their diff interpretation; gitsync only invokes their
// read-only dry-run interface and displays the result with provenance.
func DescriptionToolFor(repoType vcs.RepoType) string {
	if repoType == vcs.Jujutsu {
		return "jj-ai-commit"
	}
	return "git-ai-commit"
}

// DescriptionToolAvailable reports whether the AI description tool for path
// is installed.
func DescriptionToolAvailable(path string) bool {
	_, err := exec.LookPath(DescriptionToolFor(vcs.DetectRepoType(path)))
	return err == nil
}

// DescriptionFile is one changed path reported by an AI commit tool.
type DescriptionFile struct {
	Status string `json:"status"`
	Path   string `json:"path"`
}

// DescriptionTarget identifies the working copy the description was derived
// from. ID is populated when the owning VCS provides a stable identifier.
type DescriptionTarget struct {
	Kind    string `json:"kind"`
	Display string `json:"display"`
	ID      string `json:"id,omitempty"`
}

// ChangeDescription is gitsync's normalized view of ai-describe/v1. The
// external JSON contract is decoded once here rather than leaking into TUI
// layout code.
type ChangeDescription struct {
	Tool                string
	RequestedModel      string
	ModelDisplay        string
	Target              DescriptionTarget
	PreviousDescription string
	Description         string
	Files               []DescriptionFile
	DiffBytes           int64
	CommitMessage       string
}

type descriptionDocument struct {
	SchemaVersion string `json:"schemaVersion"`
	Tool          struct {
		Name string `json:"name"`
	} `json:"tool"`
	Model struct {
		Requested string `json:"requested"`
		Display   string `json:"display"`
	} `json:"model"`
	Results []struct {
		Target              DescriptionTarget `json:"target"`
		PreviousDescription string            `json:"previousDescription"`
		Description         string            `json:"description"`
		Files               []DescriptionFile `json:"files"`
		DiffBytes           int64             `json:"diffBytes"`
		Applied             *bool             `json:"applied"`
	} `json:"results"`
}

// DescribeChanges asks the repository-specific AI tool to describe the dirty
// changes without applying the proposed description.
func DescribeChanges(path string) (ChangeDescription, error) {
	repoType := vcs.DetectRepoType(path)
	if repoType == vcs.Dir {
		return ChangeDescription{}, fmt.Errorf("not a repository")
	}
	tool := DescriptionToolFor(repoType)
	if _, err := exec.LookPath(tool); err != nil {
		return ChangeDescription{}, fmt.Errorf("%s is not installed", tool)
	}
	args := []string{"--dry-run", "--json"}
	output, err := vcs.RunVCSOutputWithin(path, vcs.DraftTimeout, tool, args...)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if stderr := strings.TrimSpace(string(exitErr.Stderr)); stderr != "" {
				return ChangeDescription{}, fmt.Errorf("%s failed: %w\n%s", tool, err, stderr)
			}
		}
		return ChangeDescription{}, fmt.Errorf("%s failed: %w", tool, err)
	}
	if len(bytes.TrimSpace(output)) == 0 {
		return ChangeDescription{}, fmt.Errorf("%s returned no description", tool)
	}
	description, err := decodeChangeDescription(output)
	if err != nil {
		return ChangeDescription{}, fmt.Errorf("parse %s JSON: %w", tool, err)
	}
	if description.Tool != tool {
		return ChangeDescription{}, fmt.Errorf("parse %s JSON: tool.name is %q", tool, description.Tool)
	}
	return description, nil
}

func decodeChangeDescription(output []byte) (ChangeDescription, error) {
	var document descriptionDocument
	decoder := json.NewDecoder(bytes.NewReader(output))
	if err := decoder.Decode(&document); err != nil {
		return ChangeDescription{}, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return ChangeDescription{}, fmt.Errorf("multiple JSON values")
		}
		return ChangeDescription{}, fmt.Errorf("trailing data: %w", err)
	}
	if document.SchemaVersion != descriptionSchemaVersion {
		return ChangeDescription{}, fmt.Errorf("unsupported schemaVersion %q", document.SchemaVersion)
	}
	if document.Tool.Name == "" {
		return ChangeDescription{}, fmt.Errorf("tool.name is required")
	}
	if document.Model.Display == "" {
		return ChangeDescription{}, fmt.Errorf("model.display is required")
	}
	if len(document.Results) != 1 {
		return ChangeDescription{}, fmt.Errorf("expected one result, got %d", len(document.Results))
	}
	result := document.Results[0]
	if result.Target.Kind == "" || result.Target.Display == "" {
		return ChangeDescription{}, fmt.Errorf("result target kind and display are required")
	}
	if result.Applied == nil {
		return ChangeDescription{}, fmt.Errorf("result applied flag is required")
	}
	if *result.Applied {
		return ChangeDescription{}, fmt.Errorf("description command unexpectedly applied changes")
	}
	message := strings.TrimSpace(result.Description)
	if message == "" {
		return ChangeDescription{}, fmt.Errorf("result description is required")
	}
	if result.DiffBytes < 0 {
		return ChangeDescription{}, fmt.Errorf("result diffBytes must not be negative")
	}
	for i, file := range result.Files {
		if file.Status == "" || file.Path == "" {
			return ChangeDescription{}, fmt.Errorf("result file %d requires status and path", i)
		}
	}
	return ChangeDescription{
		Tool:                document.Tool.Name,
		RequestedModel:      document.Model.Requested,
		ModelDisplay:        document.Model.Display,
		Target:              result.Target,
		PreviousDescription: result.PreviousDescription,
		Description:         message,
		Files:               result.Files,
		DiffBytes:           result.DiffBytes,
		CommitMessage:       message,
	}, nil
}

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
