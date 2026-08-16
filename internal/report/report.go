package report

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"

	"github.com/osteele/gitsync/internal/vcs"
)

// StateKind categorizes a status token for coloring and summary counting.
type StateKind int

const (
	StateError StateKind = iota
	StateCorrupted
	stateDirty
	stateAhead
	stateBehind
	stateAheadUnknown
	stateNoRemote
	stateNested
	stateClean
)

// StateToken is one noteworthy state of a repository, rendered as text.
type StateToken struct {
	Kind StateKind
	Text string
}

// StatusTokens reports the noteworthy states of a repo, most urgent first.
// Scan errors and corruption crowd out everything else, since the rest of
// the status could not be reliably determined. A non-repository directory
// has no status beyond a count of the repositories it directly contains.
// Shared by the batch table and the TUI so the two agree.
func StatusTokens(s *vcs.RepoStatus) []StateToken {
	var tokens []StateToken
	if s.Error != "" {
		tokens = append(tokens, StateToken{StateError, "error: " + FirstLine(s.Error)})
	}
	if s.Corrupted {
		tokens = append(tokens, StateToken{StateCorrupted, "corrupted"})
	}
	if len(tokens) > 0 {
		return tokens
	}
	if s.Type == vcs.Dir {
		if s.NestedRepos > 0 {
			tokens = append(tokens, StateToken{stateNested, fmt.Sprintf("%d %s", s.NestedRepos, plural(s.NestedRepos, "repo"))})
		}
		return tokens
	}
	if s.Dirty {
		tokens = append(tokens, StateToken{stateDirty, "dirty"})
	}
	if s.Ahead.Positive() {
		tokens = append(tokens, StateToken{stateAhead, fmt.Sprintf("ahead %d", s.Ahead.N)})
	}
	if s.Behind.Positive() {
		tokens = append(tokens, StateToken{stateBehind, fmt.Sprintf("behind %d", s.Behind.N)})
	}
	if s.Remote {
		if !s.Ahead.Known {
			tokens = append(tokens, StateToken{stateAheadUnknown, "ahead ?"})
		}
	} else {
		tokens = append(tokens, StateToken{stateNoRemote, "no remote"})
	}
	if len(tokens) == 0 {
		tokens = append(tokens, StateToken{stateClean, "clean"})
	}
	return tokens
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func FirstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimRight(line, "\r")
}

const ansiReset = "\033[0m"

// ansiColor returns the color for a token kind: red for failures, yellow
// for uncommitted work, cyan for sync state, dim for informational, green
// for clean.
func (k StateKind) ansiColor() string {
	switch k {
	case StateError, StateCorrupted:
		return "\033[31m"
	case stateDirty:
		return "\033[33m"
	case stateAhead, stateBehind, stateAheadUnknown:
		return "\033[36m"
	case stateNoRemote, stateNested:
		return "\033[2m"
	case stateClean:
		return "\033[32m"
	}
	return ""
}

// statusText joins a repo's status tokens, colorizing each by kind when
// color is enabled.
func statusText(s *vcs.RepoStatus, color bool) string {
	tokens := StatusTokens(s)
	texts := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		text := tok.Text
		if color {
			text = tok.Kind.ansiColor() + text + ansiReset
		}
		texts = append(texts, text)
	}
	return strings.Join(texts, " ")
}

// UseColor reports whether output to w should be colorized: only when w is
// a terminal and NO_COLOR is not set.
func UseColor(w *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return term.IsTerminal(int(w.Fd()))
}

// PrepareDisplay splits results into the rows to print and the count of
// hidden non-repository directories. Non-repository directories print only
// with --all, except that one whose descendants print, or that the caller
// marked ContextOnly (e.g. because it contains repositories), is kept as
// context — it is the only way to see where its children live — and is not
// tallied by the summary.
func PrepareDisplay(results []*vcs.RepoStatus, showAll bool) (visible []*vcs.RepoStatus, hiddenBare int) {
	// Rows are ordered so that descendants immediately follow their
	// ancestors, so a right-to-left pass can tell whether a directory
	// locates any kept descendant: keptBelow[d] records that a kept row at
	// depth d has been seen within the current subtree.
	var keptBelow []bool
	keep := make([]bool, len(results))
	for i := len(results) - 1; i >= 0; i-- {
		s := results[i]
		depth := s.Depth
		if depth < 1 {
			depth = 1
		}
		keptDescendant := false
		for d := depth + 1; d < len(keptBelow); d++ {
			if keptBelow[d] {
				keptDescendant = true
				break
			}
		}
		keep[i] = s.Type != vcs.Dir || showAll || s.ContextOnly || keptDescendant
		s.ContextOnly = keep[i] && s.Type == vcs.Dir && !showAll
		for d := depth; d < len(keptBelow); d++ {
			keptBelow[d] = false
		}
		for len(keptBelow) <= depth {
			keptBelow = append(keptBelow, false)
		}
		if keep[i] {
			keptBelow[depth] = true
		}
	}

	for i, s := range results {
		if !keep[i] {
			hiddenBare++
			continue
		}
		visible = append(visible, s)
	}
	return visible, hiddenBare
}

// summaryLine tallies the displayed results, e.g.
// "18 repos: 3 dirty, 2 ahead, 1 behind, 1 no remote, 1 error". Only
// nonzero categories appear; a clean sweep prints "all clean".
func summaryLine(visible []*vcs.RepoStatus, hiddenBare int) string {
	repos := 0
	nonRepos := 0
	counts := map[StateKind]int{}
	for _, s := range visible {
		// A context-only row is shown to locate its children; it is not
		// itself part of the report.
		if s.ContextOnly {
			continue
		}
		if s.Type == vcs.Dir {
			nonRepos++
			continue
		}
		repos++
		for _, tok := range StatusTokens(s) {
			counts[tok.Kind]++
		}
	}

	if repos == 0 && nonRepos == 0 && hiddenBare == 0 {
		return "no repositories"
	}

	parts := []string{fmt.Sprintf("%d %s", repos, plural(repos, "repo"))}
	if nonRepos > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", nonRepos, plural(nonRepos, "non-repo")))
	}
	line := strings.Join(parts, ", ")
	if hiddenBare > 0 {
		line += fmt.Sprintf(" (+%d %s hidden)", hiddenBare, plural(hiddenBare, "non-repo"))
	}

	var categories []string
	for _, entry := range []struct {
		kind StateKind
		name string
	}{
		{stateDirty, "dirty"},
		{stateAhead, "ahead"},
		{stateBehind, "behind"},
		{stateNoRemote, "no remote"},
		{StateCorrupted, "corrupted"},
		{StateError, "error"},
	} {
		if n := counts[entry.kind]; n > 0 {
			categories = append(categories, fmt.Sprintf("%d %s", n, entry.name))
		}
	}
	if len(categories) == 0 {
		line += ": all clean"
	} else {
		line += ": " + strings.Join(categories, ", ")
	}
	return line
}

// PrintReport writes the batch table followed by the summary line. The
// status column is last so ANSI color codes never inflate the column
// widths that tabwriter computes.
func PrintReport(w io.Writer, visible []*vcs.RepoStatus, hiddenBare int, color bool) {
	if len(visible) > 0 {
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "Name\tVCS\tStatus")
		for _, s := range visible {
			// Children indent one level under their parent directory.
			name := filepath.Base(s.Path)
			if s.Depth > 1 {
				name = strings.Repeat("  ", s.Depth-1) + name
			}
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", name, s.Type, statusText(s, color))
		}
		_ = tw.Flush()
		_, _ = fmt.Fprintln(w)
	}
	_, _ = fmt.Fprintf(w, "%s\n", summaryLine(visible, hiddenBare))
}

// FilterExitCode maps the run outcome to the process exit code: with a
// filter, at least one match exits 1 (something needs attention) and no
// matches exit 0; without a filter the exit code is always 0.
func FilterExitCode(filterGiven bool, matches int) int {
	if filterGiven && matches > 0 {
		return 1
	}
	return 0
}
