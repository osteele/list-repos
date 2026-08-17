package vcs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

const (
	// StatusTimeout bounds local status queries that never touch the network
	// (status --porcelain, remote, rev-list, jj diff, jj log, ...).
	StatusTimeout = 10 * time.Second
	// NetworkTimeout bounds operations that talk to a remote (push, pull,
	// fetch, GitHub-remote setup, repair's fetch).
	NetworkTimeout = 120 * time.Second
	// DraftTimeout bounds the AI commit-message tools. These are not just
	// network calls: the model reads the whole diff and writes prose.
	// Measured drafts of a one-file change took 2m10s and 3m19s, so
	// NetworkTimeout would have failed most of them. The generous ceiling
	// costs nothing when the tool is quick and is why the prompt must never
	// block on it.
	DraftTimeout = 6 * time.Minute
)

// RunVCS runs a git/jj command in dir with a timeout and a non-interactive
// environment, returning combined output.
func RunVCS(dir string, timeout time.Duration, name string, args ...string) ([]byte, error) {
	return runVCSWith(dir, timeout, true, name, args...)
}

// RunVCSOutput is like RunVCS but returns stdout only, for callers that need
// Output() semantics. All such callers are local status queries, so it always
// uses StatusTimeout.
func RunVCSOutput(dir, name string, args ...string) ([]byte, error) {
	return runVCSWith(dir, StatusTimeout, false, name, args...)
}

// RunVCSOutputWithin is RunVCSOutput with an explicit timeout, for
// stdout-only commands that outlive the status budget. Stdout-only matters
// as much as the timeout here: the AI commit-message tools write a progress
// spinner to stderr, which combined output would splice into the result.
func RunVCSOutputWithin(dir string, timeout time.Duration, name string, args ...string) ([]byte, error) {
	return runVCSWith(dir, timeout, false, name, args...)
}

func runVCSWith(dir string, timeout time.Duration, combined bool, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	// Never prompt for credentials: a remote that asks for an SSH passphrase
	// or HTTPS password must fail fast, not hang a worker or TUI action
	// forever. Harmless for jj, which shells out to git/ssh for its git
	// backend.
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -oBatchMode=yes",
	)
	var output []byte
	var err error
	if combined {
		output, err = cmd.CombinedOutput()
	} else {
		output, err = cmd.Output()
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return output, fmt.Errorf("%s timed out after %v: %w", name, timeout, context.DeadlineExceeded)
	}
	return output, err
}
