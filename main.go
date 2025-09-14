package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type RepoType int

const (
	Bare RepoType = iota
	Git
	Jujutsu
)

func (rt RepoType) String() string {
	switch rt {
	case Bare:
		return "bare"
	case Git:
		return "git"
	case Jujutsu:
		return "jujutsu"
	default:
		return "unknown"
	}
}

type RepoStatus struct {
	Path   string
	Type   RepoType
	Dirty  bool
	Remote bool
	Ahead  bool
}

func formatBool(value, noUnicode bool) string {
	if noUnicode {
		if value {
			return "true"
		}
		return "false"
	}
	if value {
		return "✓"
	}
	return "✗"
}

func getDefaultDirectory() string {
	// Try jj root first (since jj repos often have .git too)
	cmd := exec.Command("jj", "root")
	output, err := cmd.Output()
	if err == nil {
		root := strings.TrimSpace(string(output))
		if root != "" {
			return root
		}
	}

	// Try git root
	cmd = exec.Command("git", "rev-parse", "--show-toplevel")
	output, err = cmd.Output()
	if err == nil {
		root := strings.TrimSpace(string(output))
		if root != "" {
			return root
		}
	}

	// Default to current directory
	return "."
}

func main() {
	noUnicode := flag.Bool("no-unicode", false, "Use text instead of Unicode symbols for boolean values")
	flag.Parse()

	// Determine which directory to scan
	var scanDir string
	args := flag.Args()
	if len(args) > 0 {
		// Use positional argument if provided
		scanDir = args[0]
	} else {
		// Use smart default: git/jj root if in a repo, otherwise current directory
		scanDir = getDefaultDirectory()
	}

	subdirs, err := getSubdirectories(scanDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("% -30s % -10s % -7s % -7s % -7s\n", "Name", "VCS", "Dirty", "Remote", "Ahead")
	for _, subdir := range subdirs {
		status, err := getRepoStatus(subdir)
		if err != nil {
			// Don't print errors for subdirectories that are not repositories
			continue
		}
		fmt.Printf("% -30s % -10s % -7s % -7s % -7s\n",
			filepath.Base(subdir),
			status.Type,
			formatBool(status.Dirty, *noUnicode),
			formatBool(status.Remote, *noUnicode),
			formatBool(status.Ahead, *noUnicode))
	}
}

func getSubdirectories(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var subdirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			name := entry.Name()
			// Skip hidden directories (starting with .) and directories starting with _
			if len(name) > 0 && (name[0] == '.' || name[0] == '_') {
				continue
			}
			subdirs = append(subdirs, filepath.Join(dir, name))
		}
	}
	return subdirs, nil
}

func getRepoStatus(dir string) (*RepoStatus, error) {
	status := &RepoStatus{
		Path: dir,
		Type: Bare,
	}

	if _, err := os.Stat(filepath.Join(dir, ".jj")); err == nil {
		status.Type = Jujutsu
		err := getJujutsuStatus(status)
		if err != nil {
			return nil, err
		}
	} else if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		status.Type = Git
		err := getGitStatus(status)
		if err != nil {
			return nil, err
		}
	}

	return status, nil
}

func getGitStatus(status *RepoStatus) error {
	// Check for uncommitted changes
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = status.Path
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get git status for %s: %w", status.Path, err)
	}
	status.Dirty = len(output) > 0

	// Check for a remote
	cmd = exec.Command("git", "remote")
	cmd.Dir = status.Path
	output, err = cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get git remote for %s: %w", status.Path, err)
	}
	status.Remote = len(output) > 0

	// Check for ahead commits
	cmd = exec.Command("git", "log", "origin/main..main")
	cmd.Dir = status.Path
	output, err = cmd.Output()
	if err != nil {
		// If origin/main doesn't exist, there are no unpushed commits
		return nil
	}
	status.Ahead = len(output) > 0

	return nil
}

func getJujutsuStatus(status *RepoStatus) error {
	// Check if current revision has files and no description (dirty)
	// First check if there are any modified files
	cmd := exec.Command("jj", "status")
	cmd.Dir = status.Path
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get jujutsu status for %s: %w\n%s", status.Path, err, output)
	}
	hasFiles := len(output) > 0

	// Check if current revision has a description
	cmd = exec.Command("jj", "log", "-r", "@", "--no-graph", "-T", "description")
	cmd.Dir = status.Path
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get jujutsu description for %s: %w\n%s", status.Path, err, output)
	}
	hasNoDescription := len(bytes.TrimSpace(output)) == 0

	// Dirty if has files and no description
	status.Dirty = hasFiles && hasNoDescription

	// Check for a remote
	cmd = exec.Command("jj", "git", "remote", "list")
	cmd.Dir = status.Path
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get jujutsu remote for %s: %w\n%s", status.Path, err, output)
	}
	status.Remote = len(output) > 0

	// Check for ahead commits (only if there's a remote)
	if status.Remote {
		// Count non-empty revisions that are not in remote bookmarks (excluding root)
		// We exclude empty revisions as they're typically just working copies
		cmd = exec.Command("jj", "log", "-r", "all() & ~ remote_bookmarks() & ~ root() & ~ empty()", "--no-graph", "-T", "commit_id")
		cmd.Dir = status.Path
		output, err = cmd.CombinedOutput()
		if err != nil {
			// If the command fails, assume no unpushed commits
			status.Ahead = false
		} else {
			// If there are any commit IDs in the output, we have ahead commits
			status.Ahead = len(bytes.TrimSpace(output)) > 0
		}
	} else {
		status.Ahead = false
	}

	return nil
}
