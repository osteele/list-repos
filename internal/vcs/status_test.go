package vcs

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGetRepoStatusPreservesCorruptedRepo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "status-corrupted")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	// Delete HEAD to make the repo appear corrupted.
	if err := os.Remove(filepath.Join(tmpDir, ".git", "HEAD")); err != nil {
		t.Fatal(err)
	}

	status, err := GetRepoStatus(tmpDir)
	if err != nil {
		t.Fatalf("expected status to be returned even for corrupted repo, got error: %v", err)
	}
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if !status.Corrupted {
		t.Fatalf("expected corrupted status, got %#v", status)
	}
	if status.Type != Git {
		t.Fatalf("expected Git type, got %v", status.Type)
	}
}

func TestGetRepoStatusFreshGitRepo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "status-fresh")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "config", "user.name", "Test")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	status, err := GetRepoStatus(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Type != Git {
		t.Fatalf("expected Git, got %v", status.Type)
	}
	if status.Dirty {
		t.Fatal("fresh repo should not be dirty")
	}
	if status.Remote {
		t.Fatal("fresh repo should not have remote")
	}
	if status.Ahead.Positive() {
		t.Fatal("fresh repo should not be ahead")
	}
	if status.Behind.Positive() {
		t.Fatal("fresh repo should not be behind")
	}
}
