package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"
)

// stateKind categorizes a status token for coloring and summary counting.
type stateKind int

const (
	stateError stateKind = iota
	stateCorrupted
	stateDirty
	stateAhead
	stateBehind
	stateAheadUnknown
	stateNoRemote
	stateClean
)

// stateToken is one noteworthy state of a repository, rendered as text.
type stateToken struct {
	kind stateKind
	text string
}

// statusTokens reports the noteworthy states of a repo, most urgent first.
// Scan errors and corruption crowd out everything else, since the rest of
// the status could not be reliably determined. Bare directories have no
// status at all. Shared by the batch table and the TUI so the two agree.
func statusTokens(s *RepoStatus) []stateToken {
	var tokens []stateToken
	if s.Error != "" {
		tokens = append(tokens, stateToken{stateError, "error: " + firstLine(s.Error)})
	}
	if s.Corrupted {
		tokens = append(tokens, stateToken{stateCorrupted, "corrupted"})
	}
	if len(tokens) > 0 || s.Type == Bare {
		return tokens
	}
	if s.Dirty {
		tokens = append(tokens, stateToken{stateDirty, "dirty"})
	}
	if s.Ahead.Positive() {
		tokens = append(tokens, stateToken{stateAhead, fmt.Sprintf("ahead %d", s.Ahead.N)})
	}
	if s.Behind.Positive() {
		tokens = append(tokens, stateToken{stateBehind, fmt.Sprintf("behind %d", s.Behind.N)})
	}
	if s.Remote {
		if !s.Ahead.Known {
			tokens = append(tokens, stateToken{stateAheadUnknown, "ahead ?"})
		}
	} else {
		tokens = append(tokens, stateToken{stateNoRemote, "no remote"})
	}
	if len(tokens) == 0 {
		tokens = append(tokens, stateToken{stateClean, "clean"})
	}
	return tokens
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimRight(line, "\r")
}

const ansiReset = "\033[0m"

// ansiColor returns the color for a token kind: red for failures, yellow
// for uncommitted work, cyan for sync state, dim for informational, green
// for clean.
func (k stateKind) ansiColor() string {
	switch k {
	case stateError, stateCorrupted:
		return "\033[31m"
	case stateDirty:
		return "\033[33m"
	case stateAhead, stateBehind, stateAheadUnknown:
		return "\033[36m"
	case stateNoRemote:
		return "\033[2m"
	case stateClean:
		return "\033[32m"
	}
	return ""
}

// statusText joins a repo's status tokens, colorizing each by kind when
// color is enabled.
func statusText(s *RepoStatus, color bool) string {
	tokens := statusTokens(s)
	texts := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		text := tok.text
		if color {
			text = tok.kind.ansiColor() + text + ansiReset
		}
		texts = append(texts, text)
	}
	return strings.Join(texts, " ")
}

// useColor reports whether output to w should be colorized: only when w is
// a terminal and NO_COLOR is not set.
func useColor(w *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return term.IsTerminal(int(w.Fd()))
}

// prepareDisplay splits results into the rows to print and the count of
// hidden bare directories. Bare directories print only with --all.
func prepareDisplay(results []*RepoStatus, showAll bool) (visible []*RepoStatus, hiddenBare int) {
	for _, s := range results {
		if s.Type == Bare && !showAll {
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
func summaryLine(visible []*RepoStatus, hiddenBare int) string {
	repos := 0
	nonRepos := 0
	counts := map[stateKind]int{}
	for _, s := range visible {
		if s.Type == Bare {
			nonRepos++
			continue
		}
		repos++
		for _, tok := range statusTokens(s) {
			counts[tok.kind]++
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
		kind stateKind
		name string
	}{
		{stateDirty, "dirty"},
		{stateAhead, "ahead"},
		{stateBehind, "behind"},
		{stateNoRemote, "no remote"},
		{stateCorrupted, "corrupted"},
		{stateError, "error"},
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

// printReport writes the batch table followed by the summary line. The
// status column is last so ANSI color codes never inflate the column
// widths that tabwriter computes.
func printReport(w io.Writer, visible []*RepoStatus, hiddenBare int, color bool) {
	if len(visible) > 0 {
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "Name\tVCS\tStatus")
		for _, s := range visible {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", filepath.Base(s.Path), s.Type, statusText(s, color))
		}
		_ = tw.Flush()
		_, _ = fmt.Fprintln(w)
	}
	_, _ = fmt.Fprintf(w, "%s\n", summaryLine(visible, hiddenBare))
}

// filterExitCode maps the run outcome to the process exit code: with a
// filter, at least one match exits 1 (something needs attention) and no
// matches exit 0; without a filter the exit code is always 0.
func filterExitCode(filterGiven bool, matches int) int {
	if filterGiven && matches > 0 {
		return 1
	}
	return 0
}
