package scan

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/osteele/gitsync/internal/vcs"
)

type repoResult struct {
	path   string
	status *vcs.RepoStatus
	err    error
}

// Options controls how directories are discovered for scanning.
type Options struct {
	// Recursive descends into non-repository directories to find nested
	// repositories. A directory that is itself a repository is never
	// descended into, so only the outermost repositories are reported.
	Recursive bool
	// MaxDepth caps how many levels below the root are listed: 1 lists
	// immediate subdirectories only. Values below 1 are treated as 1.
	MaxDepth int
}

// heavyDirs are build-output directories that are listed at the top level
// but never descended into during a recursive scan, and whose contents do
// not count toward a directory's nested repositories.
var heavyDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"target":       true,
	"build":        true,
	"dist":         true,
	"venv":         true,
}

// hiddenName reports whether a directory name is excluded from scanning:
// hidden directories (starting with .) and meta directories (starting
// with _).
func hiddenName(name string) bool {
	return len(name) > 0 && (name[0] == '.' || name[0] == '_')
}

// childDirs returns the immediate non-hidden subdirectories of dir. When
// descending is true, heavy build directories are excluded as well.
func childDirs(dir string, descending bool) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var subdirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if hiddenName(name) || (descending && heavyDirs[name]) {
			continue
		}
		subdirs = append(subdirs, filepath.Join(dir, name))
	}
	return subdirs, nil
}

// GetSubdirectories returns the immediate non-hidden subdirectories of dir.
func GetSubdirectories(dir string) ([]string, error) {
	return childDirs(dir, false)
}

// CountNestedRepos counts the immediate child directories of dir that are
// themselves repositories, from a single shallow readdir. The same names
// the scanner skips while descending do not count.
func CountNestedRepos(dir string) int {
	subdirs, err := childDirs(dir, true)
	if err != nil {
		return 0
	}
	n := 0
	for _, subdir := range subdirs {
		if vcs.DetectRepoType(subdir) != vcs.Dir {
			n++
		}
	}
	return n
}

// dirEntry is a discovered directory and its level below the scan root.
type dirEntry struct {
	path  string
	depth int
}

// discover lists the directories to scan, in breadth-first order. The
// descent rule: only directories that are not themselves repositories are
// descended into, so the outermost repositories are reported and their
// contents are not. Enumeration failures below the root are recorded per
// directory so the row can surface the error instead of silently losing
// the subtree.
func discover(root string, opts Options) ([]dirEntry, map[string]error, error) {
	maxDepth := opts.MaxDepth
	if maxDepth < 1 {
		maxDepth = 1
	}

	var entries []dirEntry
	errs := map[string]error{}
	queue := []dirEntry{{path: root, depth: 0}}
	for len(queue) > 0 {
		e := queue[0]
		queue = queue[1:]
		if e.depth >= maxDepth {
			continue
		}
		descending := opts.Recursive && e.depth > 0
		subdirs, err := childDirs(e.path, descending)
		if err != nil {
			if e.depth == 0 {
				return nil, nil, err
			}
			errs[e.path] = err
			continue
		}
		for _, subdir := range subdirs {
			entries = append(entries, dirEntry{path: subdir, depth: e.depth + 1})
			// Stop at repositories: their contents are never listed.
			// Heavy build directories are listed but never descended into.
			if opts.Recursive && !heavyDirs[filepath.Base(subdir)] && vcs.DetectRepoType(subdir) == vcs.Dir {
				queue = append(queue, dirEntry{path: subdir, depth: e.depth + 1})
			}
		}
	}
	return entries, errs, nil
}

// lessPath compares two paths segment by segment, so a parent always sorts
// immediately before its children and siblings sort by name.
func lessPath(a, b string) bool {
	as := strings.Split(filepath.Clean(a), string(filepath.Separator))
	bs := strings.Split(filepath.Clean(b), string(filepath.Separator))
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] != bs[i] {
			return as[i] < bs[i]
		}
	}
	return len(as) < len(bs)
}

// Scan discovers the subdirectories of root according to opts and scans
// them, returning statuses ordered so that a directory immediately
// precedes its children.
func Scan(root string, opts Options) ([]*vcs.RepoStatus, error) {
	entries, errs, err := discover(root, opts)
	if err != nil {
		return nil, err
	}

	depths := make(map[string]int, len(entries))
	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		depths[e.path] = e.depth
		paths = append(paths, e.path)
	}

	results, errorCount := ScanDirectories(paths)
	if errorCount > 0 {
		warnErrors(errorCount)
	}

	for _, status := range results {
		status.Depth = depths[status.Path]
		if err, ok := errs[status.Path]; ok && status.Error == "" {
			status.Error = fmt.Sprintf("failed to list contents: %v", err)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return lessPath(results[i].Path, results[j].Path)
	})

	return results, nil
}

// ScanDirectories scans each directory for VCS status in parallel with a
// bounded worker pool, returning a slice of statuses and the number of
// directories that could not be processed. Errored repos still produce a
// row so the user can see the failure.
func ScanDirectories(dirs []string) ([]*vcs.RepoStatus, int) {
	if len(dirs) == 0 {
		return nil, 0
	}

	workerCount := runtime.NumCPU()
	if workerCount < 1 {
		workerCount = 1
	}
	if len(dirs) < workerCount {
		workerCount = len(dirs)
	}

	jobs := make(chan string, len(dirs))
	resultChan := make(chan repoResult, len(dirs))

	var wg sync.WaitGroup
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer wg.Done()
			for dir := range jobs {
				status, err := vcs.GetRepoStatus(dir)
				resultChan <- repoResult{path: dir, status: status, err: err}
			}
		}()
	}

	for _, dir := range dirs {
		jobs <- dir
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	var results []*vcs.RepoStatus
	var errorCount int
	for result := range resultChan {
		if result.err != nil {
			errorCount++
			// Errored repos still get a row: a repo that cannot be scanned
			// is exactly the one the user needs to see. Corrupted stays
			// whatever damage detection found; a plain error is not damage.
			status := result.status
			if status == nil {
				status = &vcs.RepoStatus{Path: result.path, Type: vcs.Dir}
			}
			if status.Error == "" {
				status.Error = result.err.Error()
			}
			results = append(results, status)
		} else if result.status != nil {
			results = append(results, result.status)
		}
	}

	for _, status := range results {
		if status.Type == vcs.Dir {
			status.NestedRepos = CountNestedRepos(status.Path)
		}
	}

	return results, errorCount
}

func warnErrors(errorCount int) {
	fmt.Fprintf(os.Stderr, "warning: %d repositor%s could not be processed\n", errorCount, func() string {
		if errorCount == 1 {
			return "y"
		}
		return "ies"
	}())
}

// ProcessSubdirectoriesParallel scans each subdirectory for VCS status in
// parallel, returning a slice of statuses. Errored repos still produce a
// row so the user can see the failure.
func ProcessSubdirectoriesParallel(subdirs []string) []*vcs.RepoStatus {
	results, errorCount := ScanDirectories(subdirs)
	if errorCount > 0 {
		warnErrors(errorCount)
	}
	return results
}
