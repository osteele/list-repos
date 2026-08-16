package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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
	Path      string
	Type      RepoType
	Dirty     bool
	Remote    bool
	Ahead     bool
	Behind    bool
	Corrupted bool
	Error     string
}

type repoResult struct {
	status *RepoStatus
	err    error
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

func processSubdirectoriesParallel(subdirs []string) []*RepoStatus {
	if len(subdirs) == 0 {
		return nil
	}

	workerCount := runtime.NumCPU()
	if workerCount < 1 {
		workerCount = 1
	}
	if len(subdirs) < workerCount {
		workerCount = len(subdirs)
	}

	jobs := make(chan string, len(subdirs))
	resultChan := make(chan repoResult, len(subdirs))

	var wg sync.WaitGroup
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer wg.Done()
			for dir := range jobs {
				status, err := getRepoStatus(dir)
				resultChan <- repoResult{status: status, err: err}
			}
		}()
	}

	for _, subdir := range subdirs {
		jobs <- subdir
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	var results []*RepoStatus
	var errorCount int
	for result := range resultChan {
		if result.err != nil {
			errorCount++
			fmt.Fprintf(os.Stderr, "warning: failed to process repository: %v\n", result.err)
		} else if result.status != nil {
			results = append(results, result.status)
		}
	}

	if errorCount > 0 {
		fmt.Fprintf(os.Stderr, "warning: %d repositor%s could not be processed\n", errorCount, func() string {
			if errorCount == 1 {
				return "y"
			}
			return "ies"
		}())
	}

	return results
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
			// Preserve corrupted repos so batch mode can surface them.
			if status.Corrupted {
				return status, nil
			}
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
		status.Error = fmt.Sprintf("failed to get git status: %v", err)
		status.Corrupted = detectGitDamageQuick(status.Path)
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

	// Check for ahead commits relative to the current upstream if configured
	cmd = exec.Command("git", "rev-list", "--count", "@{u}..HEAD")
	cmd.Dir = status.Path
	output, err = cmd.CombinedOutput()
	if err != nil {
		// No upstream configured or unable to compare; treat as not ahead
		status.Ahead = false
	} else {
		status.Ahead = strings.TrimSpace(string(output)) != "0"
	}

	// Check for behind commits relative to the current upstream if configured
	cmd = exec.Command("git", "rev-list", "--count", "HEAD..@{u}")
	cmd.Dir = status.Path
	output, err = cmd.CombinedOutput()
	if err != nil {
		status.Behind = false
	} else {
		status.Behind = strings.TrimSpace(string(output)) != "0"
	}

	return nil
}

func getJujutsuStatus(status *RepoStatus) error {
	// Check for working copy changes using diff --summary
	cmd := exec.Command("jj", "diff", "--summary")
	cmd.Dir = status.Path
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get jujutsu diff for %s: %w\n%s", status.Path, err, output)
	}
	status.Dirty = len(bytes.TrimSpace(output)) > 0

	// Check for a remote
	// Note: This command fails for non-git-backed jj repos (created with `jj init`)
	// We treat that as "no remote" rather than an error
	cmd = exec.Command("jj", "git", "remote", "list")
	cmd.Dir = status.Path
	output, err = cmd.CombinedOutput()
	if err != nil {
		// If the command fails (e.g., no git backend), assume no remote
		status.Remote = false
	} else {
		status.Remote = len(output) > 0
	}

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

	status.Behind = false
	return nil
}
