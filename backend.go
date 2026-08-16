package main

import (
	"fmt"
	"strings"
)

// Backend is the per-VCS implementation of status checks and actions,
// selected once from the detected repo type.
type Backend interface {
	Status(st *RepoStatus) error
	Push(path string) error
	Pull(path string) error
	Commit(path, message string) error
}

// backendFor returns the Backend for a detected repo type. Bare and unknown
// types fall back to Git, matching the historical action dispatch.
func backendFor(t RepoType) Backend {
	if t == Jujutsu {
		return jjBackend{}
	}
	return gitBackend{}
}

type gitBackend struct{}

func (gitBackend) Status(st *RepoStatus) error { return getGitStatus(st) }

func (gitBackend) Push(path string) error {
	// Refuse to push if there is no remote at all.
	if !hasOrigin(path) {
		return fmt.Errorf("no origin remote configured")
	}
	// Set the upstream on first push. Detection is structural (rev-parse)
	// rather than grep of push stderr, which is locale-dependent.
	args := []string{"push"}
	if !hasUpstream(path) {
		args = []string{"push", "-u", "origin", "HEAD"}
	}
	output, err := runVCS(path, networkTimeout, "git", args...)
	if err != nil {
		return fmt.Errorf("push failed: %w\n%s", err, output)
	}
	return nil
}

func (gitBackend) Pull(path string) error {
	output, err := runVCS(path, networkTimeout, "git", "pull", "--rebase")
	if err != nil {
		return fmt.Errorf("pull failed: %w\n%s", err, output)
	}
	return nil
}

func (gitBackend) Commit(path, message string) error {
	// Stage all changes (including untracked) and commit.
	if output, err := runVCS(path, statusTimeout, "git", "add", "-A"); err != nil {
		return fmt.Errorf("git add failed: %w\n%s", err, output)
	}
	output, err := runVCS(path, statusTimeout, "git", "commit", "-m", message)
	if err != nil {
		// Nothing to commit is not a failure.
		if strings.Contains(string(output), "nothing to commit") {
			return nil
		}
		return fmt.Errorf("commit failed: %w\n%s", err, output)
	}
	return nil
}

// hasUpstream reports whether the current branch has an upstream configured.
func hasUpstream(path string) bool {
	_, err := runVCSOutput(path, "git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	return err == nil
}

type jjBackend struct{}

func (jjBackend) Status(st *RepoStatus) error { return getJujutsuStatus(st) }

func (jjBackend) Push(path string) error {
	output, err := runVCS(path, networkTimeout, "jj", "git", "push", "--all")
	if err != nil {
		return fmt.Errorf("push failed: %w\n%s", err, output)
	}
	return nil
}

func (jjBackend) Pull(path string) error {
	output, err := runVCS(path, networkTimeout, "jj", "git", "fetch")
	if err != nil {
		return fmt.Errorf("fetch failed: %w\n%s", err, output)
	}
	return nil
}

func (jjBackend) Commit(path, message string) error {
	output, err := runVCS(path, statusTimeout, "jj", "commit", "-m", message)
	if err != nil {
		return fmt.Errorf("commit failed: %w\n%s", err, output)
	}
	return nil
}
