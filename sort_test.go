package main

import (
	"path/filepath"
	"testing"
)

func TestParseSort(t *testing.T) {
	testCases := []struct {
		expr     string
		expected int // expected number of sort keys
	}{
		{"", 1},                 // Default to name
		{"name", 1},             // Single field
		{"vcs", 1},              // Single field
		{"dirty", 1},            // Single field
		{"ahead", 1},            // Single field
		{"remote", 1},           // Single field
		{"vcs,name", 2},         // Multiple fields
		{"dirty,ahead,name", 3}, // Multiple fields
		{"!dirty", 1},           // Reverse sort
		{"!dirty,name", 2},      // Mixed reverse and normal
		{"!vcs,!name", 2},       // Multiple reverse
	}

	for _, tc := range testCases {
		t.Run(tc.expr, func(t *testing.T) {
			keys, err := ParseSort(tc.expr)
			if err != nil {
				t.Fatalf("ParseSort(%q) error: %v", tc.expr, err)
			}

			if len(keys) != tc.expected {
				t.Errorf("ParseSort(%q) returned %d keys, expected %d",
					tc.expr, len(keys), tc.expected)
			}
		})
	}
}

func TestParseSortErrors(t *testing.T) {
	testCases := []string{
		"unknown",
		"name,unknown",
		"!unknown",
	}

	for _, expr := range testCases {
		t.Run(expr, func(t *testing.T) {
			_, err := ParseSort(expr)
			if err == nil {
				t.Errorf("ParseSort(%q) expected error, got nil", expr)
			}
		})
	}
}

func TestSortResults(t *testing.T) {
	// Create test data
	results := []*RepoStatus{
		{Path: "/path/zebra", Type: Git, Dirty: true, Ahead: false, Remote: true},
		{Path: "/path/alpha", Type: Jujutsu, Dirty: false, Ahead: true, Remote: false},
		{Path: "/path/beta", Type: Bare, Dirty: false, Ahead: false, Remote: false},
		{Path: "/path/gamma", Type: Git, Dirty: true, Ahead: true, Remote: true},
	}

	testCases := []struct {
		expr     string
		expected []string // expected order of names
	}{
		{"name", []string{"alpha", "beta", "gamma", "zebra"}},
		{"!name", []string{"zebra", "gamma", "beta", "alpha"}},
		{"vcs", []string{"beta", "zebra", "gamma", "alpha"}},              // Bare, Git, Git, Jujutsu
		{"dirty", []string{"zebra", "gamma", "alpha", "beta"}},            // Dirty first
		{"!dirty", []string{"alpha", "beta", "zebra", "gamma"}},           // Clean first
		{"ahead", []string{"alpha", "gamma", "zebra", "beta"}},            // Ahead first
		{"dirty,name", []string{"gamma", "zebra", "alpha", "beta"}},       // Dirty first, then by name
		{"vcs,name", []string{"beta", "gamma", "zebra", "alpha"}},         // By VCS (Bare, Git, Git, Jujutsu), then by name
		{"dirty,ahead,name", []string{"gamma", "zebra", "alpha", "beta"}}, // Complex sort
	}

	for _, tc := range testCases {
		t.Run(tc.expr, func(t *testing.T) {
			// Make a copy of results for sorting
			sortedResults := make([]*RepoStatus, len(results))
			copy(sortedResults, results)

			keys, err := ParseSort(tc.expr)
			if err != nil {
				t.Fatalf("ParseSort(%q) error: %v", tc.expr, err)
			}

			SortResults(sortedResults, keys)

			// Check order
			for i, expected := range tc.expected {
				actual := filepath.Base(sortedResults[i].Path)
				if actual != expected {
					t.Errorf("Sort %q position %d: got %s, expected %s",
						tc.expr, i, actual, expected)
				}
			}
		})
	}
}

func TestSortComparison(t *testing.T) {
	// Test individual sort key comparisons
	a := &RepoStatus{Path: "/path/a", Type: Git, Dirty: true, Ahead: true, Remote: true}
	b := &RepoStatus{Path: "/path/b", Type: Jujutsu, Dirty: false, Ahead: false, Remote: false}

	testCases := []struct {
		key      SortKey
		expected int // -1 if a < b, 0 if equal, 1 if a > b
	}{
		{&nameSortKey{reverse: false}, -1},   // a < b alphabetically
		{&nameSortKey{reverse: true}, 1},     // reversed: a > b
		{&vcsSortKey{reverse: false}, -1},    // Git < Jujutsu
		{&vcsSortKey{reverse: true}, 1},      // reversed
		{&dirtySortKey{reverse: false}, -1},  // dirty comes first
		{&dirtySortKey{reverse: true}, 1},    // clean comes first when reversed
		{&aheadSortKey{reverse: false}, -1},  // ahead comes first
		{&remoteSortKey{reverse: false}, -1}, // remote comes first
	}

	for _, tc := range testCases {
		result := tc.key.Compare(a, b)
		if result != tc.expected {
			t.Errorf("Compare(%+v, %+v) with key %T = %d, expected %d",
				a, b, tc.key, result, tc.expected)
		}
	}
}

func TestSortCaseInsensitive(t *testing.T) {
	testCases := []string{
		"NAME",
		"VCS",
		"DIRTY",
		"AHEAD",
		"REMOTE",
		"!NAME",
		"VCS,NAME",
		"!DIRTY,NAME",
	}

	for _, expr := range testCases {
		t.Run(expr, func(t *testing.T) {
			_, err := ParseSort(expr)
			if err != nil {
				t.Errorf("ParseSort(%q) should handle case-insensitive input, got error: %v", expr, err)
			}
		})
	}
}
