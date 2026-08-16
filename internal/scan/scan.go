package scan

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/osteele/gitsync/internal/vcs"
)

type repoResult struct {
	path   string
	status *vcs.RepoStatus
	err    error
}

// GetSubdirectories returns the immediate non-hidden subdirectories of dir.
func GetSubdirectories(dir string) ([]string, error) {
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

// ProcessSubdirectoriesParallel scans each subdirectory for VCS status in
// parallel, returning a slice of statuses. Errored repos still produce a row
// so the user can see the failure.
func ProcessSubdirectoriesParallel(subdirs []string) []*vcs.RepoStatus {
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
				status, err := vcs.GetRepoStatus(dir)
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
				status = &vcs.RepoStatus{Path: result.path, Type: vcs.Bare}
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
