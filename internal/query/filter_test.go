package query

import (
	"testing"

	"github.com/osteele/gitsync/internal/vcs"
)

func TestParseFilter(t *testing.T) {
	testCases := []struct {
		expr     string
		status   vcs.RepoStatus
		expected bool
	}{
		// Basic terms
		{"dirty", vcs.RepoStatus{Dirty: true}, true},
		{"dirty", vcs.RepoStatus{Dirty: false}, false},
		{"clean", vcs.RepoStatus{Dirty: false}, true},
		{"clean", vcs.RepoStatus{Dirty: true}, false},
		{"ahead", vcs.RepoStatus{Ahead: vcs.Count{N: 1, Known: true}}, true},
		{"ahead", vcs.RepoStatus{Ahead: vcs.Count{N: 0, Known: true}}, false},
		{"ahead", vcs.RepoStatus{Ahead: vcs.Count{N: 3, Known: false}}, false}, // unknown never matches
		{"behind", vcs.RepoStatus{Behind: vcs.Count{N: 2, Known: true}}, true},
		{"behind", vcs.RepoStatus{Behind: vcs.Count{N: 0, Known: true}}, false},
		{"behind", vcs.RepoStatus{Behind: vcs.Count{N: 5, Known: false}}, false}, // unknown never matches
		{"remote", vcs.RepoStatus{Remote: true}, true},
		{"remote", vcs.RepoStatus{Remote: false}, false},
		{"local", vcs.RepoStatus{Remote: false}, true},
		{"local", vcs.RepoStatus{Remote: true}, false},
		{"git", vcs.RepoStatus{Type: vcs.Git}, true},
		{"git", vcs.RepoStatus{Type: vcs.Jujutsu}, false},
		{"jj", vcs.RepoStatus{Type: vcs.Jujutsu}, true},
		{"jj", vcs.RepoStatus{Type: vcs.Git}, false},
		{"bare", vcs.RepoStatus{Type: vcs.Dir}, true},
		{"bare", vcs.RepoStatus{Type: vcs.Git}, false},

		// AND operations
		{"dirty and ahead", vcs.RepoStatus{Dirty: true, Ahead: vcs.Count{N: 1, Known: true}}, true},
		{"dirty and ahead", vcs.RepoStatus{Dirty: true, Ahead: vcs.Count{N: 0, Known: true}}, false},
		{"dirty & ahead", vcs.RepoStatus{Dirty: true, Ahead: vcs.Count{N: 1, Known: true}}, true},
		{"git & dirty", vcs.RepoStatus{Type: vcs.Git, Dirty: true}, true},
		{"git & dirty", vcs.RepoStatus{Type: vcs.Git, Dirty: false}, false},
		{"git & dirty", vcs.RepoStatus{Type: vcs.Jujutsu, Dirty: true}, false},

		// Implicit AND (adjacent terms)
		{"dirty ahead", vcs.RepoStatus{Dirty: true, Ahead: vcs.Count{N: 1, Known: true}}, true},
		{"dirty ahead", vcs.RepoStatus{Dirty: false, Ahead: vcs.Count{N: 1, Known: true}}, false},

		// OR operations
		{"dirty or ahead", vcs.RepoStatus{Dirty: true, Ahead: vcs.Count{N: 0, Known: true}}, true},
		{"dirty or ahead", vcs.RepoStatus{Dirty: false, Ahead: vcs.Count{N: 1, Known: true}}, true},
		{"dirty or ahead", vcs.RepoStatus{Dirty: false, Ahead: vcs.Count{N: 0, Known: true}}, false},
		{"dirty | ahead", vcs.RepoStatus{Dirty: true, Ahead: vcs.Count{N: 0, Known: true}}, true},
		{"git | jj", vcs.RepoStatus{Type: vcs.Git}, true},
		{"git | jj", vcs.RepoStatus{Type: vcs.Jujutsu}, true},
		{"git | jj", vcs.RepoStatus{Type: vcs.Dir}, false},

		// NOT operations
		{"not dirty", vcs.RepoStatus{Dirty: false}, true},
		{"not dirty", vcs.RepoStatus{Dirty: true}, false},
		{"!dirty", vcs.RepoStatus{Dirty: false}, true},
		{"!git", vcs.RepoStatus{Type: vcs.Jujutsu}, true},
		{"!git", vcs.RepoStatus{Type: vcs.Git}, false},

		// Complex expressions with parentheses
		{"(git | jj) & dirty", vcs.RepoStatus{Type: vcs.Git, Dirty: true}, true},
		{"(git | jj) & dirty", vcs.RepoStatus{Type: vcs.Jujutsu, Dirty: true}, true},
		{"(git | jj) & dirty", vcs.RepoStatus{Type: vcs.Dir, Dirty: true}, false},
		{"(git | jj) & dirty", vcs.RepoStatus{Type: vcs.Git, Dirty: false}, false},
		{"!(git | jj)", vcs.RepoStatus{Type: vcs.Dir}, true},
		{"!(git | jj)", vcs.RepoStatus{Type: vcs.Git}, false},

		// Mixed complexity
		{"dirty & (git | jj) & ahead", vcs.RepoStatus{Type: vcs.Git, Dirty: true, Ahead: vcs.Count{N: 1, Known: true}}, true},
		{"dirty & (git | jj) & ahead", vcs.RepoStatus{Type: vcs.Git, Dirty: true, Ahead: vcs.Count{N: 0, Known: true}}, false},
		{"(dirty & ahead) | bare", vcs.RepoStatus{Type: vcs.Dir}, true},
		{"(dirty & ahead) | bare", vcs.RepoStatus{Dirty: true, Ahead: vcs.Count{N: 1, Known: true}}, true},
		{"(dirty & ahead) | bare", vcs.RepoStatus{Dirty: true, Ahead: vcs.Count{N: 0, Known: true}, Type: vcs.Git}, false},

		// Empty filter (matches all)
		{"", vcs.RepoStatus{}, true},
		{"", vcs.RepoStatus{Dirty: true}, true},
	}

	for _, tc := range testCases {
		t.Run(tc.expr, func(t *testing.T) {
			filter, err := ParseFilter(tc.expr)
			if err != nil {
				t.Fatalf("ParseFilter(%q) error: %v", tc.expr, err)
			}

			result := filter.Match(&tc.status)
			if result != tc.expected {
				t.Errorf("ParseFilter(%q).Match(%+v) = %v, expected %v",
					tc.expr, tc.status, result, tc.expected)
			}
		})
	}
}

func TestParseFilterErrors(t *testing.T) {
	testCases := []string{
		"unknown",
		"dirty &",
		"| dirty",
		"dirty & & ahead",
		"(dirty",
		"dirty)",
		"()",
		"dirty or or ahead",
	}

	for _, expr := range testCases {
		t.Run(expr, func(t *testing.T) {
			_, err := ParseFilter(expr)
			if err == nil {
				t.Errorf("ParseFilter(%q) expected error, got nil", expr)
			}
		})
	}
}

func TestFilterCaseInsensitive(t *testing.T) {
	testCases := []struct {
		expr     string
		status   vcs.RepoStatus
		expected bool
	}{
		{"DIRTY", vcs.RepoStatus{Dirty: true}, true},
		{"Dirty", vcs.RepoStatus{Dirty: true}, true},
		{"GIT", vcs.RepoStatus{Type: vcs.Git}, true},
		{"JJ", vcs.RepoStatus{Type: vcs.Jujutsu}, true},
		{"DIRTY AND AHEAD", vcs.RepoStatus{Dirty: true, Ahead: vcs.Count{N: 1, Known: true}}, true},
		{"dirty OR ahead", vcs.RepoStatus{Dirty: false, Ahead: vcs.Count{N: 1, Known: true}}, true},
		{"NOT dirty", vcs.RepoStatus{Dirty: false}, true},
	}

	for _, tc := range testCases {
		t.Run(tc.expr, func(t *testing.T) {
			filter, err := ParseFilter(tc.expr)
			if err != nil {
				t.Fatalf("ParseFilter(%q) error: %v", tc.expr, err)
			}

			result := filter.Match(&tc.status)
			if result != tc.expected {
				t.Errorf("ParseFilter(%q).Match(%+v) = %v, expected %v",
					tc.expr, tc.status, result, tc.expected)
			}
		})
	}
}
