package vcs

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDetectGitDamageHealthy(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "damage-healthy")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	damage, err := detectGitDamage(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(damage) > 0 {
		t.Fatalf("expected healthy repo, got damage: %v", damage)
	}
}

func TestDetectGitDamageEmptyGitDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "damage-empty")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if err := os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	damage, err := detectGitDamage(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(damage) == 0 || damage[0] != EmptyGitDir {
		t.Fatalf("expected EmptyGitDir, got %v", damage)
	}
}

func TestDetectGitDamageMissingHead(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "damage-nohead")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(tmpDir, ".git", "HEAD")); err != nil {
		t.Fatal(err)
	}

	damage, err := detectGitDamage(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, d := range damage {
		if d == MissingHead {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected MissingHead in damage list, got %v", damage)
	}
}
