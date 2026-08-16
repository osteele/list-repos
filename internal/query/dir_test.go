package query

import (
	"testing"

	"github.com/osteele/gitsync/internal/vcs"
)

func TestFilterDirAndBareAlias(t *testing.T) {
	// bare is the pre-dir name for the same term; both must keep working.
	for _, expr := range []string{"dir", "bare"} {
		filter, err := ParseFilter(expr)
		if err != nil {
			t.Fatalf("ParseFilter(%q) error: %v", expr, err)
		}
		if !filter.Match(&vcs.RepoStatus{Type: vcs.Dir}) {
			t.Errorf("ParseFilter(%q) must match a non-repository directory", expr)
		}
		if filter.Match(&vcs.RepoStatus{Type: vcs.Git}) {
			t.Errorf("ParseFilter(%q) must not match a repository", expr)
		}
	}
}
