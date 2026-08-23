package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/osteele/gitsync/internal/actions"
	"github.com/osteele/gitsync/internal/report"
	"github.com/osteele/gitsync/internal/vcs"
)

// runBulk executes one bulk operation over the scanned, filtered, and
// sorted results, and returns the process exit code: 0 when nothing ran or
// everything succeeded, 1 when any repository failed, 2 for usage-level
// refusals (missing commit tools, or a confirmation that cannot happen).
func runBulk(op actions.BulkOp, results []*vcs.RepoStatus, dryRun, yes bool, stdin *os.File, stdout, stderr io.Writer) int {
	items := actions.Plan(op, results)
	if len(items) == 0 {
		_, _ = fmt.Fprintln(stdout, actions.EmptyMessage(op))
		return 0
	}

	if op == actions.BulkCommit {
		if err := actions.PreflightCommitTools(items); err != nil {
			_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
	}
	if op == actions.BulkFix {
		actions.AnnotateFixTools(items)
	}

	actions.RenderPlan(stdout, op, items)

	// --dry-run stops after the plan, except that a dry-run commit passes
	// the AI tool's own dry-run flag through so the messages that would be
	// used are visible. Either way there is no prompt.

	if dryRun {
		if op != actions.BulkCommit {
			return 0
		}
		return executeBulk(op, items, true, stdout)
	}

	if !yes {
		if !term.IsTerminal(int(stdin.Fd())) {
			_, _ = fmt.Fprintln(stderr, "stdin is not a terminal, so confirmation is impossible; re-run with --yes to proceed or --dry-run to preview")
			return 2
		}
		if !confirm(stdin, stdout) {
			_, _ = fmt.Fprintln(stdout, "Aborted.")
			return 0
		}
	}

	return executeBulk(op, items, false, stdout)
}

// confirm prompts once and accepts only y or yes.
func confirm(stdin *os.File, stdout io.Writer) bool {
	_, _ = fmt.Fprint(stdout, "Proceed? [y/N] ")
	line, _ := bufio.NewReader(stdin).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// failureLine renders a per-repository failure without doubling the verb:
// backend errors already begin with "push failed:" and the like.
func failureLine(op actions.BulkOp, err error) string {
	msg := report.FirstLine(err.Error())
	if strings.HasPrefix(msg, op.String()+" ") {
		return msg
	}
	return fmt.Sprintf("%s failed: %s", op, msg)
}

// indented reproduces a tool's dry-run output verbatim, one indented line
// at a time. The AI commit tools format for humans -- progress spinners,
// ANSI erase codes, section headers -- so scraping a single "the message"
// line out of them would break the first time they reformat. Showing what
// the tool actually said is both robust and more informative.
func indented(output string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
		b.WriteString("\n    ")
		b.WriteString(strings.TrimRight(line, "\r"))
	}
	return b.String()
}

// executeBulk runs the batch, streaming one result line per repository as
// it finishes, then a summary. It returns 1 when any repository failed.
func executeBulk(op actions.BulkOp, items []actions.PlanItem, dryRun bool, stdout io.Writer) int {
	var failedNames []string
	succeeded, failed := actions.ExecuteBulk(op, items, dryRun, func(r actions.Result) {
		name := filepath.Base(r.Item.Status.Path)
		if r.Err != nil {
			failedNames = append(failedNames, name)
			// The backend error already reads "push failed: ..."; don't
			// prefix it again.
			_, _ = fmt.Fprintf(stdout, "✗ %s  %s\n", name, failureLine(op, r.Err))
			return
		}
		if dryRun {
			if strings.TrimSpace(r.Output) == "" {
				_, _ = fmt.Fprintf(stdout, "✓ %s  would %s\n", name, op)
				return
			}
			_, _ = fmt.Fprintf(stdout, "✓ %s  would %s:%s\n", name, op, indented(r.Output))
			return
		}
		if op == actions.BulkFix && strings.TrimSpace(r.Output) != "" {
			_, _ = fmt.Fprintf(stdout, "✓ %s  %s:%s\n", name, op.PastTense(), indented(r.Output))
			return
		}
		_, _ = fmt.Fprintf(stdout, "✓ %s  %s\n", name, op.PastTense())
	})

	verb := op.PastTense()
	if dryRun {
		verb = "would " + op.String()
	}
	summary := fmt.Sprintf("%d %s, %d failed", succeeded, verb, failed)
	if len(failedNames) > 0 {
		summary += ": " + strings.Join(failedNames, ", ")
	}
	_, _ = fmt.Fprintln(stdout, summary)

	if failed > 0 {
		return 1
	}
	return 0
}
