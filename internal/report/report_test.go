package report

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/osteele/gitsync/internal/vcs"
)

func TestStatusTokens(t *testing.T) {
	testCases := []struct {
		name     string
		status   *vcs.RepoStatus
		expected []StateToken
	}{
		{
			name:     "clean repo with remote",
			status:   &vcs.RepoStatus{Type: vcs.Git, Remote: true, Ahead: vcs.Count{N: 0, Known: true}, Behind: vcs.Count{N: 0, Known: true}},
			expected: []StateToken{{stateClean, "clean"}},
		},
		{
			name:     "dirty repo",
			status:   &vcs.RepoStatus{Type: vcs.Git, Remote: true, Dirty: true, Ahead: vcs.Count{Known: true}},
			expected: []StateToken{{stateDirty, "dirty"}},
		},
		{
			name:     "ahead and behind",
			status:   &vcs.RepoStatus{Type: vcs.Git, Remote: true, Ahead: vcs.Count{N: 3, Known: true}, Behind: vcs.Count{N: 2, Known: true}},
			expected: []StateToken{{stateAhead, "ahead 3"}, {stateBehind, "behind 2"}},
		},
		{
			name:   "dirty ahead and behind in order",
			status: &vcs.RepoStatus{Type: vcs.Jujutsu, Remote: true, Dirty: true, Ahead: vcs.Count{N: 1, Known: true}, Behind: vcs.Count{N: 2, Known: true}},
			expected: []StateToken{
				{stateDirty, "dirty"},
				{stateAhead, "ahead 1"},
				{stateBehind, "behind 2"},
			},
		},
		{
			name:     "ahead unknown with remote",
			status:   &vcs.RepoStatus{Type: vcs.Git, Remote: true},
			expected: []StateToken{{stateAheadUnknown, "ahead ?"}},
		},
		{
			name:     "no remote",
			status:   &vcs.RepoStatus{Type: vcs.Git},
			expected: []StateToken{{stateNoRemote, "no remote"}},
		},
		{
			name:     "dirty jj repo without remote",
			status:   &vcs.RepoStatus{Type: vcs.Jujutsu, Dirty: true},
			expected: []StateToken{{stateDirty, "dirty"}, {stateNoRemote, "no remote"}},
		},
		{
			name:     "error uses only the first line",
			status:   &vcs.RepoStatus{Type: vcs.Git, Error: "failed to get git status\nfatal: not a git repository"},
			expected: []StateToken{{StateError, "error: failed to get git status"}},
		},
		{
			name:     "error crowds out other states",
			status:   &vcs.RepoStatus{Type: vcs.Git, Remote: true, Dirty: true, Error: "boom"},
			expected: []StateToken{{StateError, "error: boom"}},
		},
		{
			name:     "corrupted",
			status:   &vcs.RepoStatus{Type: vcs.Git, Corrupted: true},
			expected: []StateToken{{StateCorrupted, "corrupted"}},
		},
		{
			name:     "corrupted with error",
			status:   &vcs.RepoStatus{Type: vcs.Git, Corrupted: true, Error: "bad object"},
			expected: []StateToken{{StateError, "error: bad object"}, {StateCorrupted, "corrupted"}},
		},
		{
			name:     "bare directory has no status",
			status:   &vcs.RepoStatus{Type: vcs.Dir},
			expected: nil,
		},
		{
			name:     "bare directory with a scan error still shows the error",
			status:   &vcs.RepoStatus{Type: vcs.Dir, Error: "permission denied"},
			expected: []StateToken{{StateError, "error: permission denied"}},
		},
	}

	for _, tc := range testCases {
		result := StatusTokens(tc.status)
		if !reflect.DeepEqual(result, tc.expected) {
			t.Errorf("%s: StatusTokens(%+v) = %+v, expected %+v", tc.name, tc.status, result, tc.expected)
		}
	}
}

func TestStatusText(t *testing.T) {
	dirty := &vcs.RepoStatus{Type: vcs.Git, Remote: true, Dirty: true, Ahead: vcs.Count{N: 1, Known: true}}
	if got := statusText(dirty, false); got != "dirty ahead 1" {
		t.Errorf("statusText(dirty, false) = %q, expected %q", got, "dirty ahead 1")
	}

	colored := statusText(dirty, true)
	if !strings.Contains(colored, "\033[33mdirty\033[0m") {
		t.Errorf("statusText(dirty, true) = %q, expected dirty in yellow", colored)
	}
	if !strings.Contains(colored, "\033[36mahead 1\033[0m") {
		t.Errorf("statusText(dirty, true) = %q, expected ahead count in cyan", colored)
	}

	if got := statusText(&vcs.RepoStatus{Type: vcs.Git, Remote: true, Ahead: vcs.Count{Known: true}}, true); got != "\033[32mclean\033[0m" {
		t.Errorf("statusText(clean, true) = %q, expected clean in green", got)
	}
	if got := statusText(&vcs.RepoStatus{Type: vcs.Git, Error: "boom"}, true); got != "\033[31merror: boom\033[0m" {
		t.Errorf("statusText(error, true) = %q, expected error in red", got)
	}
	if got := statusText(&vcs.RepoStatus{Type: vcs.Git}, true); got != "\033[2mno remote\033[0m" {
		t.Errorf("statusText(no remote, true) = %q, expected no remote dimmed", got)
	}
}

func TestSummaryLine(t *testing.T) {
	clean := &vcs.RepoStatus{Type: vcs.Git, Remote: true, Ahead: vcs.Count{Known: true}}

	testCases := []struct {
		name       string
		visible    []*vcs.RepoStatus
		hiddenBare int
		expected   string
	}{
		{
			name:     "all clean",
			visible:  []*vcs.RepoStatus{clean, clean},
			expected: "2 repos: all clean",
		},
		{
			name: "only nonzero categories, in order",
			visible: []*vcs.RepoStatus{
				{Type: vcs.Git, Remote: true, Dirty: true, Ahead: vcs.Count{Known: true}},
				{Type: vcs.Git, Dirty: true},
				{Type: vcs.Git, Remote: true, Ahead: vcs.Count{N: 2, Known: true}, Behind: vcs.Count{N: 1, Known: true}},
				{Type: vcs.Git, Error: "boom"},
			},
			expected: "4 repos: 2 dirty, 1 ahead, 1 behind, 1 no remote, 1 error",
		},
		{
			name:       "hidden non-repos",
			visible:    []*vcs.RepoStatus{clean},
			hiddenBare: 3,
			expected:   "1 repo (+3 non-repos hidden): all clean",
		},
		{
			name: "bare directories shown with --all count as non-repos",
			visible: []*vcs.RepoStatus{
				{Type: vcs.Git, Dirty: true},
				{Type: vcs.Dir},
				{Type: vcs.Dir},
			},
			expected: "1 repo, 2 non-repos: 1 dirty, 1 no remote",
		},
		{
			name:     "corrupted counts separately from error",
			visible:  []*vcs.RepoStatus{{Type: vcs.Git, Corrupted: true, Error: "bad object"}},
			expected: "1 repo: 1 corrupted, 1 error",
		},
		{
			// A filter that matches nothing must not read as a clean sweep.
			name:     "nothing to report",
			visible:  nil,
			expected: "no repositories",
		},
	}

	for _, tc := range testCases {
		result := summaryLine(tc.visible, tc.hiddenBare)
		if result != tc.expected {
			t.Errorf("%s: summaryLine() = %q, expected %q", tc.name, result, tc.expected)
		}
	}
}

func TestFilterExitCode(t *testing.T) {
	testCases := []struct {
		filterGiven bool
		matches     int
		expected    int
	}{
		{true, 3, 1},
		{true, 1, 1},
		{true, 0, 0},
		{false, 0, 0},
		{false, 5, 0},
	}

	for _, tc := range testCases {
		result := FilterExitCode(tc.filterGiven, tc.matches)
		if result != tc.expected {
			t.Errorf("FilterExitCode(%v, %d) = %d, expected %d", tc.filterGiven, tc.matches, result, tc.expected)
		}
	}
}

func TestPrepareDisplay(t *testing.T) {
	repo := &vcs.RepoStatus{Type: vcs.Git}
	bare := &vcs.RepoStatus{Type: vcs.Dir}
	results := []*vcs.RepoStatus{repo, bare, bare}

	visible, hidden := PrepareDisplay(results, false)
	if len(visible) != 1 || visible[0] != repo {
		t.Errorf("PrepareDisplay(_, false) kept %d rows, expected only the repo", len(visible))
	}
	if hidden != 2 {
		t.Errorf("PrepareDisplay(_, false) hid %d bare dirs, expected 2", hidden)
	}

	visible, hidden = PrepareDisplay(results, true)
	if len(visible) != 3 {
		t.Errorf("PrepareDisplay(_, true) kept %d rows, expected all 3", len(visible))
	}
	if hidden != 0 {
		t.Errorf("PrepareDisplay(_, true) reported %d hidden, expected 0", hidden)
	}
}

func TestPrintReport(t *testing.T) {
	longName := "a-repository-name-well-over-thirty-characters-long"
	visible := []*vcs.RepoStatus{
		{Path: "/tmp/scan/" + longName, Type: vcs.Git, Remote: true, Ahead: vcs.Count{Known: true}},
		{Path: "/tmp/scan/todo", Type: vcs.Git, Remote: true, Dirty: true, Ahead: vcs.Count{N: 2, Known: true}},
		{Path: "/tmp/scan/junk", Type: vcs.Dir},
	}

	var buf bytes.Buffer
	PrintReport(&buf, visible, 0, false)
	output := buf.String()

	if strings.Contains(output, "\033[") {
		t.Errorf("colorless output contains ANSI codes:\n%s", output)
	}

	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if !strings.HasPrefix(lines[0], "Name") || !strings.Contains(lines[0], "VCS") || !strings.Contains(lines[0], "Status") {
		t.Errorf("unexpected header line %q", lines[0])
	}

	// Every column must start at the same offset on every row, even with a
	// name longer than the old fixed 30-character width.
	vcsOffset := -1
	statusOffset := -1
	for _, line := range lines[1:4] {
		vcs := strings.Index(line, "git")
		if vcs == -1 {
			vcs = strings.Index(line, "dir")
		}
		if vcsOffset == -1 {
			vcsOffset = vcs
		} else if vcs != vcsOffset {
			t.Errorf("VCS column starts at %d in %q, expected %d", vcs, line, vcsOffset)
		}

		status := -1
		for _, token := range []string{"clean", "dirty ahead 2"} {
			if i := strings.Index(line, token); i != -1 {
				status = i
			}
		}
		if status == -1 {
			continue // the bare row has an empty status
		}
		if statusOffset == -1 {
			statusOffset = status
		} else if status != statusOffset {
			t.Errorf("status column starts at %d in %q, expected %d", status, line, statusOffset)
		}
	}
	if vcsOffset == -1 || statusOffset == -1 {
		t.Fatalf("could not locate columns in output:\n%s", output)
	}

	if !strings.HasSuffix(output, "2 repos, 1 non-repo: 1 dirty, 1 ahead\n") {
		t.Errorf("output does not end with the expected summary line:\n%s", output)
	}

	buf.Reset()
	PrintReport(&buf, visible[:1], 0, true)
	if !strings.Contains(buf.String(), "\033[32mclean\033[0m") {
		t.Errorf("colored output lacks ANSI codes:\n%s", buf.String())
	}
}
