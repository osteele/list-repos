package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestStatusTokens(t *testing.T) {
	testCases := []struct {
		name     string
		status   *RepoStatus
		expected []stateToken
	}{
		{
			name:     "clean repo with remote",
			status:   &RepoStatus{Type: Git, Remote: true, Ahead: Count{N: 0, Known: true}, Behind: Count{N: 0, Known: true}},
			expected: []stateToken{{stateClean, "clean"}},
		},
		{
			name:     "dirty repo",
			status:   &RepoStatus{Type: Git, Remote: true, Dirty: true, Ahead: Count{Known: true}},
			expected: []stateToken{{stateDirty, "dirty"}},
		},
		{
			name:     "ahead and behind",
			status:   &RepoStatus{Type: Git, Remote: true, Ahead: Count{N: 3, Known: true}, Behind: Count{N: 2, Known: true}},
			expected: []stateToken{{stateAhead, "ahead 3"}, {stateBehind, "behind 2"}},
		},
		{
			name:   "dirty ahead and behind in order",
			status: &RepoStatus{Type: Jujutsu, Remote: true, Dirty: true, Ahead: Count{N: 1, Known: true}, Behind: Count{N: 2, Known: true}},
			expected: []stateToken{
				{stateDirty, "dirty"},
				{stateAhead, "ahead 1"},
				{stateBehind, "behind 2"},
			},
		},
		{
			name:     "ahead unknown with remote",
			status:   &RepoStatus{Type: Git, Remote: true},
			expected: []stateToken{{stateAheadUnknown, "ahead ?"}},
		},
		{
			name:     "no remote",
			status:   &RepoStatus{Type: Git},
			expected: []stateToken{{stateNoRemote, "no remote"}},
		},
		{
			name:     "dirty jj repo without remote",
			status:   &RepoStatus{Type: Jujutsu, Dirty: true},
			expected: []stateToken{{stateDirty, "dirty"}, {stateNoRemote, "no remote"}},
		},
		{
			name:     "error uses only the first line",
			status:   &RepoStatus{Type: Git, Error: "failed to get git status\nfatal: not a git repository"},
			expected: []stateToken{{stateError, "error: failed to get git status"}},
		},
		{
			name:     "error crowds out other states",
			status:   &RepoStatus{Type: Git, Remote: true, Dirty: true, Error: "boom"},
			expected: []stateToken{{stateError, "error: boom"}},
		},
		{
			name:     "corrupted",
			status:   &RepoStatus{Type: Git, Corrupted: true},
			expected: []stateToken{{stateCorrupted, "corrupted"}},
		},
		{
			name:     "corrupted with error",
			status:   &RepoStatus{Type: Git, Corrupted: true, Error: "bad object"},
			expected: []stateToken{{stateError, "error: bad object"}, {stateCorrupted, "corrupted"}},
		},
		{
			name:     "bare directory has no status",
			status:   &RepoStatus{Type: Bare},
			expected: nil,
		},
		{
			name:     "bare directory with a scan error still shows the error",
			status:   &RepoStatus{Type: Bare, Error: "permission denied"},
			expected: []stateToken{{stateError, "error: permission denied"}},
		},
	}

	for _, tc := range testCases {
		result := statusTokens(tc.status)
		if !reflect.DeepEqual(result, tc.expected) {
			t.Errorf("%s: statusTokens(%+v) = %+v, expected %+v", tc.name, tc.status, result, tc.expected)
		}
	}
}

func TestStatusText(t *testing.T) {
	dirty := &RepoStatus{Type: Git, Remote: true, Dirty: true, Ahead: Count{N: 1, Known: true}}
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

	if got := statusText(&RepoStatus{Type: Git, Remote: true, Ahead: Count{Known: true}}, true); got != "\033[32mclean\033[0m" {
		t.Errorf("statusText(clean, true) = %q, expected clean in green", got)
	}
	if got := statusText(&RepoStatus{Type: Git, Error: "boom"}, true); got != "\033[31merror: boom\033[0m" {
		t.Errorf("statusText(error, true) = %q, expected error in red", got)
	}
	if got := statusText(&RepoStatus{Type: Git}, true); got != "\033[2mno remote\033[0m" {
		t.Errorf("statusText(no remote, true) = %q, expected no remote dimmed", got)
	}
}

func TestSummaryLine(t *testing.T) {
	clean := &RepoStatus{Type: Git, Remote: true, Ahead: Count{Known: true}}

	testCases := []struct {
		name       string
		visible    []*RepoStatus
		hiddenBare int
		expected   string
	}{
		{
			name:     "all clean",
			visible:  []*RepoStatus{clean, clean},
			expected: "2 repos: all clean",
		},
		{
			name: "only nonzero categories, in order",
			visible: []*RepoStatus{
				{Type: Git, Remote: true, Dirty: true, Ahead: Count{Known: true}},
				{Type: Git, Dirty: true},
				{Type: Git, Remote: true, Ahead: Count{N: 2, Known: true}, Behind: Count{N: 1, Known: true}},
				{Type: Git, Error: "boom"},
			},
			expected: "4 repos: 2 dirty, 1 ahead, 1 behind, 1 no remote, 1 error",
		},
		{
			name:       "hidden non-repos",
			visible:    []*RepoStatus{clean},
			hiddenBare: 3,
			expected:   "1 repo (+3 non-repos hidden): all clean",
		},
		{
			name: "bare directories shown with --all count as non-repos",
			visible: []*RepoStatus{
				{Type: Git, Dirty: true},
				{Type: Bare},
				{Type: Bare},
			},
			expected: "1 repo, 2 non-repos: 1 dirty, 1 no remote",
		},
		{
			name:     "corrupted counts separately from error",
			visible:  []*RepoStatus{{Type: Git, Corrupted: true, Error: "bad object"}},
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
		result := filterExitCode(tc.filterGiven, tc.matches)
		if result != tc.expected {
			t.Errorf("filterExitCode(%v, %d) = %d, expected %d", tc.filterGiven, tc.matches, result, tc.expected)
		}
	}
}

func TestPrepareDisplay(t *testing.T) {
	repo := &RepoStatus{Type: Git}
	bare := &RepoStatus{Type: Bare}
	results := []*RepoStatus{repo, bare, bare}

	visible, hidden := prepareDisplay(results, false)
	if len(visible) != 1 || visible[0] != repo {
		t.Errorf("prepareDisplay(_, false) kept %d rows, expected only the repo", len(visible))
	}
	if hidden != 2 {
		t.Errorf("prepareDisplay(_, false) hid %d bare dirs, expected 2", hidden)
	}

	visible, hidden = prepareDisplay(results, true)
	if len(visible) != 3 {
		t.Errorf("prepareDisplay(_, true) kept %d rows, expected all 3", len(visible))
	}
	if hidden != 0 {
		t.Errorf("prepareDisplay(_, true) reported %d hidden, expected 0", hidden)
	}
}

func TestPrintReport(t *testing.T) {
	longName := "a-repository-name-well-over-thirty-characters-long"
	visible := []*RepoStatus{
		{Path: "/tmp/scan/" + longName, Type: Git, Remote: true, Ahead: Count{Known: true}},
		{Path: "/tmp/scan/todo", Type: Git, Remote: true, Dirty: true, Ahead: Count{N: 2, Known: true}},
		{Path: "/tmp/scan/junk", Type: Bare},
	}

	var buf bytes.Buffer
	printReport(&buf, visible, 0, false)
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
			vcs = strings.Index(line, "bare")
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
	printReport(&buf, visible[:1], 0, true)
	if !strings.Contains(buf.String(), "\033[32mclean\033[0m") {
		t.Errorf("colored output lacks ANSI codes:\n%s", buf.String())
	}
}
