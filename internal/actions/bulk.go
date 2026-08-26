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
	// BulkFix runs jj fix in every healthy Jujutsu repository.
	BulkFix
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
	case BulkFix:
		return "fix"
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
	case BulkFix:
		return "processed"
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
	case BulkFix:
		return s.Type == vcs.Jujutsu && s.Error == "" && !s.Corrupted
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
	case BulkFix:
		return "Jujutsu repository"
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

// AnnotateFixTools replaces each fix plan item's generic reason with the
// effective formatter names reported by jj's versioned config interface.
// Discovery failures remain visible in the plan and do not suppress sibling
// repositories; execution will independently report the same repository's
// failure.
func AnnotateFixTools(items []PlanItem) {
	for i := range items {
		tools, err := ConfiguredFixTools(items[i].Status.Path)
		switch {
		case err != nil:
			items[i].Reason = "tools unknown: " + firstLine(err.Error())
		case len(tools) == 0:
			items[i].Reason = "no configured tools"
		default:
			items[i].Reason = strings.Join(tools, ", ")
		}
	}
}

func firstLine(s string) string {
	if line, _, ok := strings.Cut(s, "\n"); ok {
		return line
	}
	return s
}

// ConfiguredFixTools asks jj for the effective fix configuration and returns
// the tool names. It consumes CLI output instead of reading Jujutsu's config
// files, whose locations and merge rules belong to Jujutsu.
func ConfiguredFixTools(path string) ([]string, error) {
	output, err := vcs.RunVCSOutput(path, "jj", "config", "list", "fix.tools", "-T", `name ++ "\n"`)
	if err != nil {
		return nil, fmt.Errorf("list jj fix tools: %w", err)
	}
	return parseFixToolNames(string(output))
}

func parseFixToolNames(output string) ([]string, error) {
	seen := map[string]bool{}
	var tools []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		const prefix = "fix.tools."
		nameAndField, ok := strings.CutPrefix(line, prefix)
		if !ok {
			return nil, fmt.Errorf("unexpected jj config key %q", line)
		}
		name, _, ok := strings.Cut(nameAndField, ".")
		if !ok || name == "" {
			return nil, fmt.Errorf("unexpected jj fix tool key %q", line)
		}
		if !seen[name] {
			seen[name] = true
			tools = append(tools, name)
		}
	}
	sort.Strings(tools)
	return tools, nil
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
	Item             PlanItem
	Output           string
	ChangedRevisions int
	Err              error
}

// ExecuteBulk runs the operation on every planned item through a bounded
// worker pool, streaming each result to onResult in completion order. Fix is
// deliberately sequential because each jj process may itself launch several
// formatters; the other operations use the scanner's CPU-sized bound. A
// failure in one repository does not abort the others. dryRun only affects
// commit, which passes the AI tool's own dry-run flag through so the would-be
// messages are shown.
func ExecuteBulk(op BulkOp, items []PlanItem, dryRun bool, onResult func(Result)) (succeeded, failed int) {
	return ExecuteBulkWithProgress(op, items, dryRun, nil, onResult)
}

// ExecuteBulkWithProgress is ExecuteBulk with an additional callback invoked
// when a worker actually starts a repository. Both callbacks are serialized by
// the coordinator, so display clients can consume real queued/running/completed
// transitions without synchronizing their own state.
func ExecuteBulkWithProgress(op BulkOp, items []PlanItem, dryRun bool, onStart func(PlanItem), onResult func(Result)) (succeeded, failed int) {
	if len(items) == 0 {
		return 0, 0
	}
	workerCount := workerCountFor(op, len(items))

	jobs := make(chan PlanItem)
	type executionEvent struct {
		item    PlanItem
		result  Result
		started bool
	}
	events := make(chan executionEvent, len(items)*2)
	var wg sync.WaitGroup
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer wg.Done()
			for item := range jobs {
				events <- executionEvent{item: item, started: true}
				output, changedRevisions, err := runOne(op, item.Status.Path, dryRun)
				events <- executionEvent{result: Result{Item: item, Output: output, ChangedRevisions: changedRevisions, Err: err}}
			}
		}()
	}
	go func() {
		for _, item := range items {
			jobs <- item
		}
		close(jobs)
		wg.Wait()
		close(events)
	}()

	for event := range events {
		if event.started {
			if onStart != nil {
				onStart(event.item)
			}
			continue
		}
		r := event.result
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

func workerCountFor(op BulkOp, itemCount int) int {
	if op == BulkFix {
		return 1
	}
	workerCount := runtime.NumCPU()
	if workerCount < 1 {
		workerCount = 1
	}
	if itemCount < workerCount {
		workerCount = itemCount
	}
	return workerCount
}

func runOne(op BulkOp, path string, dryRun bool) (string, int, error) {
	switch op {
	case BulkCommit:
		output, err := actionCommitAI(path, dryRun)
		return output, 0, err
	case BulkPull:
		return "", 0, ActionPull(path)
	case BulkFix:
		result, err := ActionFix(path)
		return result.Output, result.ChangedRevisions, err
	case BulkPush:
		return "", 0, ActionPush(path)
	case BulkSync:
		return "", 0, ActionSync(path)
	default:
		return "", 0, fmt.Errorf("unknown bulk operation")
	}
}

// FixResult reports both the review details and the number of revisions whose
// commit IDs changed in the jj fix operation.
type FixResult struct {
	Output           string
	ChangedRevisions int
}

// ActionFix runs Jujutsu's configured fix tools and reports the operation
// that contains the rewrite. The operation is Jujutsu's recoverable
// checkpoint; the printed command gives the user a direct patch review path.
func ActionFix(path string) (FixResult, error) {
	tools, err := ConfiguredFixTools(path)
	if err != nil {
		return FixResult{}, err
	}
	if len(tools) == 0 {
		return FixResult{}, fmt.Errorf("no jj fix tools configured")
	}
	output, err := vcs.RunVCS(path, vcs.FixTimeout, "jj", "fix")
	if err != nil {
		return FixResult{}, fmt.Errorf("jj fix failed: %w\n%s", err, output)
	}
	opOutput, err := vcs.RunVCSOutput(path, "jj", "op", "log", "--at-op=@", "--ignore-working-copy", "-G", "-n", "1", "-T", `id ++ "\n"`)
	if err != nil {
		return FixResult{}, fmt.Errorf("jj fix completed but its operation could not be identified: %w", err)
	}
	opID := strings.TrimSpace(string(opOutput))
	parentOutput, err := vcs.RunVCSOutput(path, "jj", "op", "log", "--at-op="+opID, "--ignore-working-copy", "-G", "-n", "1", "-T", `parents.map(|parent| parent.id() ++ "\n").join("")`)
	if err != nil {
		return FixResult{}, fmt.Errorf("jj fix completed in operation %s but its parent operation could not be identified: %w", opID, err)
	}
	parents := strings.Fields(string(parentOutput))
	if len(parents) != 1 {
		return FixResult{}, fmt.Errorf("jj fix completed in operation %s with %d parent operations; revision changes cannot be counted", opID, len(parents))
	}
	changedRevisions, err := countChangedRevisions(path, parents[0], opID)
	if err != nil {
		return FixResult{}, fmt.Errorf("jj fix completed in operation %s but its changed revisions could not be counted: %w", opID, err)
	}
	stat, err := vcs.RunVCS(path, vcs.StatusTimeout, "jj", "op", "show", "--at-op=@", "--ignore-working-copy", "--stat", opID)
	if err != nil {
		return FixResult{}, fmt.Errorf("jj fix completed in operation %s but its diff could not be shown: %w\n%s", opID, err, stat)
	}
	parts := []string{strings.TrimSpace(string(output)), strings.TrimSpace(string(stat))}
	var visible []string
	for _, part := range parts {
		if part != "" {
			visible = append(visible, part)
		}
	}
	visible = append(visible, fmt.Sprintf("Review: jj -R %q op show -p %s", path, opID))
	return FixResult{Output: strings.Join(visible, "\n"), ChangedRevisions: changedRevisions}, nil
}

func countChangedRevisions(path, beforeOp, afterOp string) (int, error) {
	before, err := revisionIDsAtOperation(path, beforeOp)
	if err != nil {
		return 0, err
	}
	after, err := revisionIDsAtOperation(path, afterOp)
	if err != nil {
		return 0, err
	}
	changeIDs := make(map[string]struct{}, len(before)+len(after))
	for id := range before {
		changeIDs[id] = struct{}{}
	}
	for id := range after {
		changeIDs[id] = struct{}{}
	}
	changed := 0
	for id := range changeIDs {
		if !equalStringSets(before[id], after[id]) {
			changed++
		}
	}
	return changed, nil
}

func revisionIDsAtOperation(path, opID string) (map[string]map[string]struct{}, error) {
	output, err := vcs.RunVCS(path, vcs.StatusTimeout, "jj", "log", "--at-op="+opID, "--ignore-working-copy", "-r", "all()", "--no-graph", "-T", `change_id ++ " " ++ commit_id ++ "\n"`)
	if err != nil {
		return nil, err
	}
	revisions := make(map[string]map[string]struct{})
	for lineNumber, line := range strings.Split(string(output), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("unexpected jj log record on line %d: %q", lineNumber+1, line)
		}
		if revisions[fields[0]] == nil {
			revisions[fields[0]] = make(map[string]struct{})
		}
		revisions[fields[0]][fields[1]] = struct{}{}
	}
	return revisions, nil
}

func equalStringSets(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for value := range a {
		if _, ok := b[value]; !ok {
			return false
		}
	}
	return true
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
