package scan

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/osteele/gitsync/internal/vcs"
)

// initGitRepo creates a git repository at path, including parents.
func initGitRepo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = path
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
}

func scanPaths(results []*vcs.RepoStatus) map[string]*vcs.RepoStatus {
	byPath := make(map[string]*vcs.RepoStatus, len(results))
	for _, s := range results {
		byPath[s.Path] = s
	}
	return byPath
}

func TestScanDescendsOnlyIntoNonRepositories(t *testing.T) {
	root := t.TempDir()

	// A repo at depth 1, a repo at depth 2 under a plain directory, and a
	// repo nested inside another repo.
	initGitRepo(t, filepath.Join(root, "repo1"))
	initGitRepo(t, filepath.Join(root, "plain", "repo2"))
	initGitRepo(t, filepath.Join(root, "repo3"))
	initGitRepo(t, filepath.Join(root, "repo3", "inner"))

	results, err := Scan(root, Options{Recursive: true, MaxDepth: 4})
	if err != nil {
		t.Fatal(err)
	}
	byPath := scanPaths(results)

	for _, path := range []string{"repo1", "plain", "plain/repo2", "repo3"} {
		if _, ok := byPath[filepath.Join(root, filepath.FromSlash(path))]; !ok {
			t.Errorf("expected %s to be reported", path)
		}
	}
	// Descent stops at repositories: a repo nested inside another repo is
	// never reported.
	if _, ok := byPath[filepath.Join(root, "repo3", "inner")]; ok {
		t.Error("a repo nested inside another repo must not be reported")
	}

	if got := byPath[filepath.Join(root, "repo1")].Depth; got != 1 {
		t.Errorf("repo1 depth = %d, expected 1", got)
	}
	if got := byPath[filepath.Join(root, "plain", "repo2")].Depth; got != 2 {
		t.Errorf("plain/repo2 depth = %d, expected 2", got)
	}

	// Parents immediately precede their children.
	parent, child := -1, -1
	for i, s := range results {
		switch s.Path {
		case filepath.Join(root, "plain"):
			parent = i
		case filepath.Join(root, "plain", "repo2"):
			child = i
		}
	}
	if parent == -1 || child != parent+1 {
		t.Errorf("expected plain immediately before plain/repo2, got indexes %d and %d", parent, child)
	}
}

func TestScanDefaultListsImmediateSubdirectoriesOnly(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, filepath.Join(root, "plain", "repo"))

	// Mirror main's invocation: the default depth stays 4 even when
	// recursion is off, so the Recursive flag alone must gate the descent.
	results, err := Scan(root, Options{Recursive: false, MaxDepth: 4})
	if err != nil {
		t.Fatal(err)
	}
	byPath := scanPaths(results)
	if _, ok := byPath[filepath.Join(root, "plain")]; !ok {
		t.Error("expected the immediate subdirectory to be reported")
	}
	if _, ok := byPath[filepath.Join(root, "plain", "repo")]; ok {
		t.Error("a nested repo must not be reported without -r")
	}
}

func TestScanDepthCap(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, filepath.Join(root, "a", "b", "repo"))

	results, err := Scan(root, Options{Recursive: true, MaxDepth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := scanPaths(results)[filepath.Join(root, "a", "b", "repo")]; ok {
		t.Error("a repo below the depth cap must not be reported")
	}

	results, err = Scan(root, Options{Recursive: true, MaxDepth: 3})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := scanPaths(results)[filepath.Join(root, "a", "b", "repo")]; !ok {
		t.Error("a repo at exactly the depth cap must be reported")
	}
}

func TestScanSkipsHeavyDirectories(t *testing.T) {
	root := t.TempDir()
	for _, heavy := range []string{"node_modules", "vendor", "target", "build", "dist", "venv"} {
		initGitRepo(t, filepath.Join(root, heavy, "repo"))
	}

	results, err := Scan(root, Options{Recursive: true, MaxDepth: 4})
	if err != nil {
		t.Fatal(err)
	}
	byPath := scanPaths(results)
	for _, heavy := range []string{"node_modules", "vendor", "target", "build", "dist", "venv"} {
		if _, ok := byPath[filepath.Join(root, heavy, "repo")]; ok {
			t.Errorf("a repo under %s must not be reported", heavy)
		}
	}
}

func TestCountNestedRepos(t *testing.T) {
	root := t.TempDir()
	container := filepath.Join(root, "container")
	initGitRepo(t, filepath.Join(container, "one"))
	initGitRepo(t, filepath.Join(container, "two"))
	initGitRepo(t, filepath.Join(container, "three"))
	// A plain child directory and a hidden repo do not count.
	if err := os.Mkdir(filepath.Join(container, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, filepath.Join(container, ".hidden-repo"))
	empty := filepath.Join(root, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := CountNestedRepos(container); got != 3 {
		t.Errorf("CountNestedRepos(container) = %d, expected 3", got)
	}
	if got := CountNestedRepos(empty); got != 0 {
		t.Errorf("CountNestedRepos(empty) = %d, expected 0", got)
	}

	// The scan populates the count on the directory's status row.
	results, err := Scan(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	byPath := scanPaths(results)
	if got := byPath[container].NestedRepos; got != 3 {
		t.Errorf("container NestedRepos = %d, expected 3", got)
	}
	if got := byPath[empty].NestedRepos; got != 0 {
		t.Errorf("empty NestedRepos = %d, expected 0", got)
	}
}
