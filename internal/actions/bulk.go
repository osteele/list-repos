package actions

import (
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/osteele/gitsync/internal/vcs"
)

// BulkOp identifies a bulk operation over every eligible repository in
// scope. Eligibility is computed from already-scanned statuses, so a bulk
// run acts on exactly the repositories the equivalent listing would print.
type BulkOp int

const (
	// BulkCommit commits every dirty repository.
	BulkCommit BulkOp = iota
	// BulkPull pulls every repository with a remote that is behind or whose
	// behind count is unknown (worth a fetch to find out).
	BulkPull
	// BulkPush pushes every repository with a remote and known unpushed
	// commits.
	BulkPush
	// BulkSync syncs every repository eligible for pull or push.
	BulkSync
)

// String is the operation's verb, used in plans and result lines.
func (op BulkOp) String() string {
	switch op {
	case BulkCommit:
		return "commit"
	case BulkPull:
		return "pull"
	case BulkPush:
		return "push"
	case BulkSync:
		return "sync"
	default:
		return "unknown"
	}
}

// PastTense is the verb form used in per-repo result lines and summaries.
func (op BulkOp) PastTense() string {
	switch op {
	case BulkCommit:
		return "committed"
	case BulkPull:
		return "pulled"
	case BulkPush:
		return "pushed"
	case BulkSync:
		return "synced"
	default:
		return "done"
	}
}

// Eligible reports whether a scanned repository is in scope for the
// operation. Non-repository directories never are. Unknown counts are not
// coerced: pull treats an unknown behind count as worth a fetch, while push
// requires a known positive ahead count — pushing on a guess could be
// wrong, fetching never is.
func Eligible(op BulkOp, s *vcs.RepoStatus) bool {
	if s.Type == vcs.Dir {
		return false
	}
	switch op {
	case BulkCommit:
		return s.Dirty
	case BulkPull:
		return s.Remote && (s.Behind.Positive() || !s.Behind.Known)
	case BulkPush:
		return s.Remote && s.Ahead.Positive()
	case BulkSync:
		return Eligible(BulkPull, s) || Eligible(BulkPush, s)
	default:
		return false
	}
}

// PlanItem is one repository selected for a bulk operation, with the reason
// it was selected, for display in the plan.
type PlanItem struct {
	Status *vcs.RepoStatus
	Reason string
}

// Plan selects the eligible repositories, preserving the listing order.
func Plan(op BulkOp, statuses []*vcs.RepoStatus) []PlanItem {
	var items []PlanItem
	for _, s := range statuses {
		if Eligible(op, s) {
			items = append(items, PlanItem{Status: s, Reason: reasonFor(op, s)})
		}
	}
	return items
}

func reasonFor(op BulkOp, s *vcs.RepoStatus) string {
	behind := func() string {
		if s.Behind.Positive() {
			return fmt.Sprintf("behind %d", s.Behind.N)
		}
		return "behind ?"
	}
	switch op {
	case BulkCommit:
		return "dirty"
	case BulkPull:
		return behind()
	case BulkPush:
		return fmt.Sprintf("ahead %d", s.Ahead.N)
	case BulkSync:
		var parts []string
		if s.Ahead.Positive() {
			parts = append(parts, fmt.Sprintf("ahead %d", s.Ahead.N))
		}
		if s.Behind.Positive() || !s.Behind.Known {
			parts = append(parts, behind())
		}
		return strings.Join(parts, ", ")
	default:
		return ""
	}
}

// EmptyMessage is printed when no repository is eligible.
func EmptyMessage(op BulkOp) string {
	return fmt.Sprintf("Nothing to %s.", op)
}

// RenderPlan writes the plan: what will happen, to which repositories, and
// why, before anything is done.
func RenderPlan(w io.Writer, op BulkOp, items []PlanItem) {
	noun := "repositories"
	if len(items) == 1 {
		noun = "repository"
	}
	_, _ = fmt.Fprintf(w, "Will %s %d %s:\n", op, len(items), noun)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, item := range items {
		_, _ = fmt.Fprintf(tw, "  %s\t%s\n", filepath.Base(item.Status.Path), item.Reason)
	}
	_ = tw.Flush()
}

// CommitToolFor names the AI commit-message tool a repository type
// requires. A bulk commit never reuses the single-repo canned message, so
// these tools are mandatory rather than optional.
func CommitToolFor(t vcs.RepoType) string {
	if t == vcs.Jujutsu {
		return "jj-ai-commit"
	}
	return "git-ai-commit"
}

// PreflightCommitTools verifies that the AI commit tool for every repo type
// in the plan is on PATH, before anything runs. A missing tool refuses the
// whole batch — a partial run would leave the user with an unexplained
// remainder.
func PreflightCommitTools(items []PlanItem) error {
	missing := map[string][]string{}
	for _, item := range items {
		tool := CommitToolFor(item.Status.Type)
		if _, err := exec.LookPath(tool); err != nil {
			missing[tool] = append(missing[tool], filepath.Base(item.Status.Path))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	tools := make([]string, 0, len(missing))
	for tool := range missing {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	var lines []string
	for _, tool := range tools {
		lines = append(lines, fmt.Sprintf("%s is required for bulk commit but is not on PATH (needed by: %s)",
			tool, strings.Join(missing[tool], ", ")))
	}
	return fmt.Errorf("%s", strings.Join(lines, "\n"))
}

// Result is the outcome of running the operation on one repository. Output
// carries the action's combined output where it is worth showing — the AI
// tools' dry-run prints the message they would commit with.
type Result struct {
	Item   PlanItem
	Output string
	Err    error
}

// ExecuteBulk runs the operation on every planned item concurrently through
// a bounded worker pool (the same bound the scanner uses), streaming each
// result to onResult in completion order. A failure in one repository does
// not abort the others. dryRun only affects commit, which passes the AI
// tool's own dry-run flag through so the would-be messages are shown.
func ExecuteBulk(op BulkOp, items []PlanItem, dryRun bool, onResult func(Result)) (succeeded, failed int) {
	if len(items) == 0 {
		return 0, 0
	}
	workerCount := runtime.NumCPU()
	if workerCount < 1 {
		workerCount = 1
	}
	if len(items) < workerCount {
		workerCount = len(items)
	}

	jobs := make(chan PlanItem)
	results := make(chan Result, len(items))
	var wg sync.WaitGroup
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer wg.Done()
			for item := range jobs {
				output, err := runOne(op, item.Status.Path, dryRun)
				results <- Result{Item: item, Output: output, Err: err}
			}
		}()
	}
	go func() {
		for _, item := range items {
			jobs <- item
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	for r := range results {
		if r.Err != nil {
			failed++
		} else {
			succeeded++
		}
		if onResult != nil {
			onResult(r)
		}
	}
	return succeeded, failed
}

func runOne(op BulkOp, path string, dryRun bool) (string, error) {
	switch op {
	case BulkCommit:
		return actionCommitAI(path, dryRun)
	case BulkPull:
		return "", ActionPull(path)
	case BulkPush:
		return "", ActionPush(path)
	case BulkSync:
		return "", ActionSync(path)
	default:
		return "", fmt.Errorf("unknown bulk operation")
	}
}

// actionCommitAI commits with an AI-generated message from git-ai-commit or
// jj-ai-commit. These call an LLM API, so they run under the network
// timeout, not the short status timeout. With dryRun the message is drafted
// and printed but nothing is committed.
func actionCommitAI(path string, dryRun bool) (string, error) {
	if dryRun {
		return DraftCommitMessage(path)
	}
	tool := CommitToolFor(vcs.DetectRepoType(path))
	output, err := vcs.RunVCS(path, vcs.DraftTimeout, tool)
	if err != nil {
		return "", fmt.Errorf("%s failed: %w\n%s", tool, err, output)
	}
	return string(output), nil
}

// CommitToolAvailable reports whether the AI commit-message tool for the
// repository at path is on PATH.
func CommitToolAvailable(path string) bool {
	_, err := exec.LookPath(CommitToolFor(vcs.DetectRepoType(path)))
	return err == nil
}

// DraftCommitMessage returns a proposed commit message for the repository
// at path, without committing anything.
//
// It asks the tool for the message alone via --print-message rather than
// scraping one out of --dry-run's human-facing report, which interleaves
// section headings and a progress spinner and would break the first time
// that output was reformatted. The call reaches an LLM API, so it runs
// under the network timeout.
func DraftCommitMessage(path string) (string, error) {
	tool := CommitToolFor(vcs.DetectRepoType(path))
	output, err := vcs.RunVCSOutputWithin(path, vcs.DraftTimeout, tool, "--print-message")
	if err != nil {
		return "", fmt.Errorf("%s failed: %w", tool, err)
	}
	return strings.TrimSpace(string(output)), nil
}
