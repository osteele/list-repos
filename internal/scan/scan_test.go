package scan

import (
	"bytes"
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

// TestScanWritesNothingToStderr guards the TUI: it runs on an alt screen,
// where a stray stderr write from a background rollup scan corrupts the
// display. Every errored directory already carries an error row, so
// announcing a count is the caller's decision, not Scan's.
func TestScanWritesNothingToStderr(t *testing.T) {
	root, err := os.MkdirTemp("", "scan-stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(root) }()

	// A garbage .jj directory makes the status check fail outright.
	broken := filepath.Join(root, "broken")
	if err := os.MkdirAll(filepath.Join(broken, ".jj"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, ".jj", "garbage"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	results, scanErr := Scan(root, Options{Recursive: true, MaxDepth: 3})
	os.Stderr = orig
	_ = w.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	if scanErr != nil {
		t.Fatalf("scan failed: %v", scanErr)
	}
	if buf.Len() != 0 {
		t.Fatalf("Scan wrote to stderr: %q", buf.String())
	}
	// The failure is still reported, just in the row rather than on stderr.
	var sawError bool
	for _, s := range results {
		if s.Error != "" {
			sawError = true
		}
	}
	if !sawError {
		t.Fatal("expected the errored directory to carry an error row")
	}
}
