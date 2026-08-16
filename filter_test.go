package main

import (
	"testing"
)

func TestParseFilter(t *testing.T) {
	testCases := []struct {
		expr     string
		status   RepoStatus
		expected bool
	}{
		// Basic terms
		{"dirty", RepoStatus{Dirty: true}, true},
		{"dirty", RepoStatus{Dirty: false}, false},
		{"clean", RepoStatus{Dirty: false}, true},
		{"clean", RepoStatus{Dirty: true}, false},
		{"ahead", RepoStatus{Ahead: Count{N: 1, Known: true}}, true},
		{"ahead", RepoStatus{Ahead: Count{N: 0, Known: true}}, false},
		{"ahead", RepoStatus{Ahead: Count{N: 3, Known: false}}, false}, // unknown never matches
		{"behind", RepoStatus{Behind: Count{N: 2, Known: true}}, true},
		{"behind", RepoStatus{Behind: Count{N: 0, Known: true}}, false},
		{"behind", RepoStatus{Behind: Count{N: 5, Known: false}}, false}, // unknown never matches
		{"remote", RepoStatus{Remote: true}, true},
		{"remote", RepoStatus{Remote: false}, false},
		{"local", RepoStatus{Remote: false}, true},
		{"local", RepoStatus{Remote: true}, false},
		{"git", RepoStatus{Type: Git}, true},
		{"git", RepoStatus{Type: Jujutsu}, false},
		{"jj", RepoStatus{Type: Jujutsu}, true},
		{"jj", RepoStatus{Type: Git}, false},
		{"bare", RepoStatus{Type: Bare}, true},
		{"bare", RepoStatus{Type: Git}, false},

		// AND operations
		{"dirty and ahead", RepoStatus{Dirty: true, Ahead: Count{N: 1, Known: true}}, true},
		{"dirty and ahead", RepoStatus{Dirty: true, Ahead: Count{N: 0, Known: true}}, false},
		{"dirty & ahead", RepoStatus{Dirty: true, Ahead: Count{N: 1, Known: true}}, true},
		{"git & dirty", RepoStatus{Type: Git, Dirty: true}, true},
		{"git & dirty", RepoStatus{Type: Git, Dirty: false}, false},
		{"git & dirty", RepoStatus{Type: Jujutsu, Dirty: true}, false},

		// Implicit AND (adjacent terms)
		{"dirty ahead", RepoStatus{Dirty: true, Ahead: Count{N: 1, Known: true}}, true},
		{"dirty ahead", RepoStatus{Dirty: false, Ahead: Count{N: 1, Known: true}}, false},

		// OR operations
		{"dirty or ahead", RepoStatus{Dirty: true, Ahead: Count{N: 0, Known: true}}, true},
		{"dirty or ahead", RepoStatus{Dirty: false, Ahead: Count{N: 1, Known: true}}, true},
		{"dirty or ahead", RepoStatus{Dirty: false, Ahead: Count{N: 0, Known: true}}, false},
		{"dirty | ahead", RepoStatus{Dirty: true, Ahead: Count{N: 0, Known: true}}, true},
		{"git | jj", RepoStatus{Type: Git}, true},
		{"git | jj", RepoStatus{Type: Jujutsu}, true},
		{"git | jj", RepoStatus{Type: Bare}, false},

		// NOT operations
		{"not dirty", RepoStatus{Dirty: false}, true},
		{"not dirty", RepoStatus{Dirty: true}, false},
		{"!dirty", RepoStatus{Dirty: false}, true},
		{"!git", RepoStatus{Type: Jujutsu}, true},
		{"!git", RepoStatus{Type: Git}, false},

		// Complex expressions with parentheses
		{"(git | jj) & dirty", RepoStatus{Type: Git, Dirty: true}, true},
		{"(git | jj) & dirty", RepoStatus{Type: Jujutsu, Dirty: true}, true},
		{"(git | jj) & dirty", RepoStatus{Type: Bare, Dirty: true}, false},
		{"(git | jj) & dirty", RepoStatus{Type: Git, Dirty: false}, false},
		{"!(git | jj)", RepoStatus{Type: Bare}, true},
		{"!(git | jj)", RepoStatus{Type: Git}, false},

		// Mixed complexity
		{"dirty & (git | jj) & ahead", RepoStatus{Type: Git, Dirty: true, Ahead: Count{N: 1, Known: true}}, true},
		{"dirty & (git | jj) & ahead", RepoStatus{Type: Git, Dirty: true, Ahead: Count{N: 0, Known: true}}, false},
		{"(dirty & ahead) | bare", RepoStatus{Type: Bare}, true},
		{"(dirty & ahead) | bare", RepoStatus{Dirty: true, Ahead: Count{N: 1, Known: true}}, true},
		{"(dirty & ahead) | bare", RepoStatus{Dirty: true, Ahead: Count{N: 0, Known: true}, Type: Git}, false},

		// Empty filter (matches all)
		{"", RepoStatus{}, true},
		{"", RepoStatus{Dirty: true}, true},
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
		status   RepoStatus
		expected bool
	}{
		{"DIRTY", RepoStatus{Dirty: true}, true},
		{"Dirty", RepoStatus{Dirty: true}, true},
		{"GIT", RepoStatus{Type: Git}, true},
		{"JJ", RepoStatus{Type: Jujutsu}, true},
		{"DIRTY AND AHEAD", RepoStatus{Dirty: true, Ahead: Count{N: 1, Known: true}}, true},
		{"dirty OR ahead", RepoStatus{Dirty: false, Ahead: Count{N: 1, Known: true}}, true},
		{"NOT dirty", RepoStatus{Dirty: false}, true},
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
