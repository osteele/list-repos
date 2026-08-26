package vcs

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// jujutsuAheadRevset selects non-empty revisions that the push action can
// publish. Keep this aligned with jjBackend.Push: that action advances the
// closest bookmark to the working-copy parent before pushing all bookmarks.
const jujutsuAheadRevset = "((::bookmarks() | ::@-) ~ ::remote_bookmarks()) & ~ root() & ~ empty()"

// RepoType classifies a directory as a non-repo, Git repo, or Jujutsu repo.
type RepoType int

const (
	// Dir is a directory that is neither a Git nor a Jujutsu repository.
	Dir RepoType = iota
	// Git is a Git repository.
	Git
	// Jujutsu is a Jujutsu repository.
	Jujutsu
)

func (rt RepoType) String() string {
	switch rt {
	case Dir:
		return "dir"
	case Git:
		return "git"
	case Jujutsu:
		return "jujutsu"
	default:
		return "unknown"
	}
}

// Count is a commit count (ahead/behind) that may be unknown when it could
// not be determined — e.g. a repo with no upstream, or a failed query.
type Count struct {
	N     int
	Known bool
}

// Positive reports whether the count is known to be greater than zero.
func (c Count) Positive() bool {
	return c.Known && c.N > 0
}

// RepoStatus is the collected VCS status for one directory.
type RepoStatus struct {
	Path      string
	Type      RepoType
	Dirty     bool
	Remote    bool
	Ahead     Count
	Behind    Count
	Corrupted bool
	Error     string
	// NestedRepos counts immediate child directories that are themselves
	// repositories. It is only populated for non-repository directories.
	NestedRepos int
	// Depth is the directory's level below the scan root: 1 for an
	// immediate subdirectory. Zero means the root level, for statuses
	// built outside a recursive scan.
	Depth int
	// ContextOnly marks a row kept only to locate its visible descendants:
	// it is displayed but excluded from the summary tallies.
	ContextOnly bool
}

// GetDefaultDirectory returns the repository root to scan when no directory is
// given on the command line. It prefers a Jujutsu root over a Git root and
// falls back to the current directory.
func GetDefaultDirectory() string {
	// Try jj root first (since jj repos often have .git too)
	output, err := RunVCSOutput(".", "jj", "root")
	if err == nil {
		root := strings.TrimSpace(string(output))
		if root != "" {
			return root
		}
	}

	// Try git root
	output, err = RunVCSOutput(".", "git", "rev-parse", "--show-toplevel")
	if err == nil {
		root := strings.TrimSpace(string(output))
		if root != "" {
			return root
		}
	}

	// Default to current directory
	return "."
}

// DetectRepoType stats the directory once to classify it. jj is checked
// first since jj repos often have a colocated .git too.
func DetectRepoType(dir string) RepoType {
	if _, err := os.Stat(filepath.Join(dir, ".jj")); err == nil {
		return Jujutsu
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return Git
	}
	return Dir
}

// GetRepoStatus returns the status for a single directory.
func GetRepoStatus(dir string) (*RepoStatus, error) {
	status := &RepoStatus{
		Path: dir,
		Type: DetectRepoType(dir),
	}
	if status.Type == Dir {
		return status, nil
	}
	if err := BackendFor(status.Type).Status(status); err != nil {
		// Preserve corrupted repos so batch mode can surface them.
		if status.Corrupted {
			return status, nil
		}
		return nil, err
	}
	return status, nil
}

func getGitStatus(status *RepoStatus) error {
	// Check for uncommitted changes
	output, err := RunVCSOutput(status.Path, "git", "status", "--porcelain")
	if err != nil {
		status.Error = fmt.Sprintf("failed to get git status: %v", err)
		status.Corrupted = detectGitDamageQuick(status.Path)
		return fmt.Errorf("failed to get git status for %s: %w", status.Path, err)
	}
	status.Dirty = len(output) > 0

	// Check for a remote
	output, err = RunVCSOutput(status.Path, "git", "remote")
	if err != nil {
		return fmt.Errorf("failed to get git remote for %s: %w", status.Path, err)
	}
	status.Remote = len(output) > 0

	// Check for ahead commits relative to the current upstream if configured
	output, err = RunVCS(status.Path, StatusTimeout, "git", "rev-list", "--count", "@{u}..HEAD")
	status.Ahead = parseCount(output, err)

	// Check for behind commits relative to the current upstream if configured
	output, err = RunVCS(status.Path, StatusTimeout, "git", "rev-list", "--count", "HEAD..@{u}")
	status.Behind = parseCount(output, err)

	return nil
}

// parseCount converts `git rev-list --count` output into a Count. Any failure
// (no upstream configured, unreadable repo) yields an unknown count rather
// than a confident zero.
func parseCount(output []byte, err error) Count {
	if err != nil {
		return Count{}
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		return Count{}
	}
	return Count{N: n, Known: true}
}

func getJujutsuStatus(status *RepoStatus) error {
	// Check for working copy changes using diff --summary
	output, err := RunVCS(status.Path, StatusTimeout, "jj", "diff", "--summary")
	if err != nil {
		return fmt.Errorf("failed to get jujutsu diff for %s: %w\n%s", status.Path, err, output)
	}
	status.Dirty = len(bytes.TrimSpace(output)) > 0

	// Check for a remote
	// Note: This command fails for non-git-backed jj repos (created with `jj init`)
	// We treat that as "no remote" rather than an error
	output, err = RunVCS(status.Path, StatusTimeout, "jj", "git", "remote", "list")
	if err != nil {
		// If the command fails (e.g., no git backend), assume no remote
		status.Remote = false
	} else {
		status.Remote = len(output) > 0
	}

	// Check for ahead commits (only if there's a remote)
	if status.Remote {
		// Count non-empty revisions the push action can publish: ancestors of
		// local bookmarks plus the current working-copy parent (which `tug`
		// advances a bookmark to), excluding history already reachable from a
		// remote bookmark. Using all() here counts unrelated visible history and
		// continues reporting a repository ahead after a successful push.
		status.Ahead = getJujutsuAheadCount(status.Path)
	} else {
		// No remote to be ahead of: the count is undefined, not zero.
		status.Ahead = Count{}
	}

	// jj does not track behind here yet; report it as unknown, not zero.
	status.Behind = Count{}
	return nil
}

func getJujutsuAheadCount(path string) Count {
	output, err := RunVCS(path, StatusTimeout, "jj", "log", "-r", jujutsuAheadRevset, "--no-graph", "-T", `commit_id ++ "\n"`)
	if err != nil {
		return Count{}
	}
	return Count{N: countLines(output), Known: true}
}

func countLines(output []byte) int {
	n := 0
	for _, line := range strings.Split(string(output), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}
