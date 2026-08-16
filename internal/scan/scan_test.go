package scan

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGetSubdirectories(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create subdirectories
	_ = os.Mkdir(filepath.Join(tmpDir, "dir1"), 0o755)
	_ = os.Mkdir(filepath.Join(tmpDir, "dir2"), 0o755)
	// Create hidden directory (should be ignored)
	_ = os.Mkdir(filepath.Join(tmpDir, ".hidden"), 0o755)
	// Create directory starting with _ (should be ignored)
	_ = os.Mkdir(filepath.Join(tmpDir, "_internal"), 0o755)
	// Create a file, which should be ignored
	_ = os.WriteFile(filepath.Join(tmpDir, "file1"), []byte(""), 0o644)

	subdirs, err := GetSubdirectories(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(subdirs) != 2 {
		t.Errorf("expected 2 subdirectories, got %d", len(subdirs))
	}
}

func TestProcessSubdirectoriesParallelIncludesCorrupted(t *testing.T) {
	parentDir, err := os.MkdirTemp("", "parallel-corrupted")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(parentDir) }()

	repoDir := filepath.Join(parentDir, "broken")
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repoDir, ".git", "HEAD")); err != nil {
		t.Fatal(err)
	}

	results := ProcessSubdirectoriesParallel([]string{repoDir})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Corrupted {
		t.Fatalf("expected corrupted result, got %#v", results[0])
	}
}

func TestProcessSubdirectoriesParallelIncludesErrored(t *testing.T) {
	parentDir, err := os.MkdirTemp("", "parallel-errored")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(parentDir) }()

	// A garbage .jj directory makes the status check fail outright; the
	// repo must still yield a row rather than being dropped.
	repoDir := filepath.Join(parentDir, "errored")
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	jjDir := filepath.Join(repoDir, ".jj")
	if err := os.Mkdir(jjDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jjDir, "garbage"), []byte("not a repo"), 0o644); err != nil {
		t.Fatal(err)
	}

	results := ProcessSubdirectoriesParallel([]string{repoDir})
	if len(results) != 1 {
		t.Fatalf("expected 1 result for the errored repo, got %d", len(results))
	}
	if results[0].Error == "" {
		t.Fatalf("expected error message on result, got %#v", results[0])
	}
	if results[0].Corrupted {
		t.Fatal("a scan error is not corruption")
	}
}
