package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
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

type RepoStatus struct {
	Path      string
	Type      RepoType
	Dirty     bool
	Remote    bool
	Ahead     Count
	Behind    Count
	Corrupted bool
	Error     string
}

type repoResult struct {
	path   string
	status *RepoStatus
	err    error
}

func getDefaultDirectory() string {
	// Try jj root first (since jj repos often have .git too)
	output, err := runVCSOutput(".", "jj", "root")
	if err == nil {
		root := strings.TrimSpace(string(output))
		if root != "" {
			return root
		}
	}

	// Try git root
	output, err = runVCSOutput(".", "git", "rev-parse", "--show-toplevel")
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
				resultChan <- repoResult{path: dir, status: status, err: err}
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
			// Errored repos still get a row: a repo that cannot be scanned
			// is exactly the one the user needs to see. Corrupted stays
			// whatever damage detection found; a plain error is not damage.
			status := result.status
			if status == nil {
				status = &RepoStatus{Path: result.path, Type: Bare}
			}
			if status.Error == "" {
				status.Error = result.err.Error()
			}
			results = append(results, status)
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

// detectRepoType stats the directory once to classify it. jj is checked
// first since jj repos often have a colocated .git too.
func detectRepoType(dir string) RepoType {
	if _, err := os.Stat(filepath.Join(dir, ".jj")); err == nil {
		return Jujutsu
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return Git
	}
	return Bare
}

func getRepoStatus(dir string) (*RepoStatus, error) {
	status := &RepoStatus{
		Path: dir,
		Type: detectRepoType(dir),
	}
	if status.Type == Bare {
		return status, nil
	}
	if err := backendFor(status.Type).Status(status); err != nil {
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
	output, err := runVCSOutput(status.Path, "git", "status", "--porcelain")
	if err != nil {
		status.Error = fmt.Sprintf("failed to get git status: %v", err)
		status.Corrupted = detectGitDamageQuick(status.Path)
		return fmt.Errorf("failed to get git status for %s: %w", status.Path, err)
	}
	status.Dirty = len(output) > 0

	// Check for a remote
	output, err = runVCSOutput(status.Path, "git", "remote")
	if err != nil {
		return fmt.Errorf("failed to get git remote for %s: %w", status.Path, err)
	}
	status.Remote = len(output) > 0

	// Check for ahead commits relative to the current upstream if configured
	output, err = runVCS(status.Path, statusTimeout, "git", "rev-list", "--count", "@{u}..HEAD")
	status.Ahead = parseCount(output, err)

	// Check for behind commits relative to the current upstream if configured
	output, err = runVCS(status.Path, statusTimeout, "git", "rev-list", "--count", "HEAD..@{u}")
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
	output, err := runVCS(status.Path, statusTimeout, "jj", "diff", "--summary")
	if err != nil {
		return fmt.Errorf("failed to get jujutsu diff for %s: %w\n%s", status.Path, err, output)
	}
	status.Dirty = len(bytes.TrimSpace(output)) > 0

	// Check for a remote
	// Note: This command fails for non-git-backed jj repos (created with `jj init`)
	// We treat that as "no remote" rather than an error
	output, err = runVCS(status.Path, statusTimeout, "jj", "git", "remote", "list")
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
		output, err = runVCS(status.Path, statusTimeout, "jj", "log", "-r", "all() & ~ remote_bookmarks() & ~ root() & ~ empty()", "--no-graph", "-T", "commit_id")
		if err != nil {
			// If the command fails, the unpushed count is unknown
			status.Ahead = Count{}
		} else {
			status.Ahead = Count{N: countLines(output), Known: true}
		}
	} else {
		// No remote to be ahead of: the count is undefined, not zero.
		status.Ahead = Count{}
	}

	// jj does not track behind here yet; report it as unknown, not zero.
	status.Behind = Count{}
	return nil
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
