package query

import (
	"path/filepath"
	"testing"

	"github.com/osteele/gitsync/internal/vcs"
)

func TestSortKeepsParentsWithChildren(t *testing.T) {
	// Sorting orders each sibling group but must never break the
	// parent/child grouping: a container's children always immediately
	// follow it.
	results := []*vcs.RepoStatus{
		{Path: "/r/repo", Type: vcs.Git, Depth: 1},
		{Path: "/r/container", Type: vcs.Dir, Depth: 1},
		{Path: "/r/container/b", Type: vcs.Git, Depth: 2},
		{Path: "/r/container/a", Type: vcs.Git, Depth: 2},
	}

	testCases := []struct {
		expr     string
		expected []string // expected order of paths relative to /r
	}{
		{"name", []string{"container", "container/a", "container/b", "repo"}},
		{"!name", []string{"repo", "container", "container/b", "container/a"}},
	}

	for _, tc := range testCases {
		t.Run(tc.expr, func(t *testing.T) {
			sorted := make([]*vcs.RepoStatus, len(results))
			copy(sorted, results)

			keys, err := ParseSort(tc.expr)
			if err != nil {
				t.Fatalf("ParseSort(%q) error: %v", tc.expr, err)
			}
			SortResults(sorted, keys)

			for i, expected := range tc.expected {
				actual, err := filepath.Rel("/r", sorted[i].Path)
				if err != nil {
					t.Fatal(err)
				}
				if actual != expected {
					t.Errorf("sort %q position %d: got %s, expected %s", tc.expr, i, actual, expected)
				}
			}
		})
	}
}
