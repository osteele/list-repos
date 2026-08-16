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
