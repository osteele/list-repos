package vcs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunVCSTimeout(t *testing.T) {
	// A command that outlives its timeout must fail with a timeout error.
	_, err := RunVCS("", 100*time.Millisecond, "sleep", "2")
	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected error wrapping context.DeadlineExceeded, got: %v", err)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected error to mention the timeout, got: %v", err)
	}
}

func TestRunVCSEnv(t *testing.T) {
	// Commands must run with credential prompts disabled.
	output, err := RunVCS("", StatusTimeout, "sh", "-c", "echo $GIT_TERMINAL_PROMPT")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(output)) != "0" {
		t.Fatalf("expected GIT_TERMINAL_PROMPT=0, got %q", output)
	}

	output, err = RunVCS("", StatusTimeout, "sh", "-c", "echo $GIT_SSH_COMMAND")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "BatchMode=yes") {
		t.Fatalf("expected GIT_SSH_COMMAND to enable BatchMode, got %q", output)
	}
}
