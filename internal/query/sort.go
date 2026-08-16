package query

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/osteele/gitsync/internal/vcs"
)

// SortKey represents a field to sort by
type SortKey interface {
	Compare(a, b *vcs.RepoStatus) int
}

// ParseSort parses a sort expression string into a list of SortKeys
func ParseSort(expr string) ([]SortKey, error) {
	if expr == "" {
		// Default sort by name
		return []SortKey{&nameSortKey{reverse: false}}, nil
	}

	fields := strings.Split(expr, ",")
	keys := make([]SortKey, 0, len(fields))

	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}

		reverse := false
		if strings.HasPrefix(field, "!") {
			reverse = true
			field = field[1:]
		}

		var key SortKey
		switch strings.ToLower(field) {
		case "name":
			key = &nameSortKey{reverse: reverse}
		case "vcs", "type":
			key = &vcsSortKey{reverse: reverse}
		case "dirty":
			key = &dirtySortKey{reverse: reverse}
		case "ahead":
			key = &aheadSortKey{reverse: reverse}
		case "behind":
			key = &behindSortKey{reverse: reverse}
		case "remote":
			key = &remoteSortKey{reverse: reverse}
		default:
			return nil, fmt.Errorf("unknown sort field: %s", field)
		}

		keys = append(keys, key)
	}

	if len(keys) == 0 {
		// Default sort by name if no valid fields
		return []SortKey{&nameSortKey{reverse: false}}, nil
	}

	return keys, nil
}

// SortResults sorts repository statuses according to the given sort keys.
// Sorting applies within each sibling group: a directory's children are
// ordered among themselves but always immediately follow their parent, so
// sorting never breaks the parent/child grouping. A row whose parent is
// absent from the list sorts as a root.
func SortResults(results []*vcs.RepoStatus, keys []SortKey) {
	present := make(map[string]bool, len(results))
	for _, s := range results {
		present[s.Path] = true
	}

	var roots []*vcs.RepoStatus
	children := make(map[string][]*vcs.RepoStatus)
	for _, s := range results {
		if parent := filepath.Dir(s.Path); present[parent] {
			children[parent] = append(children[parent], s)
		} else {
			roots = append(roots, s)
		}
	}

	byKeys := func(group []*vcs.RepoStatus) {
		sort.SliceStable(group, func(i, j int) bool {
			for _, key := range keys {
				cmp := key.Compare(group[i], group[j])
				if cmp < 0 {
					return true
				} else if cmp > 0 {
					return false
				}
				// If equal, continue to next sort key
			}
			return false
		})
	}
	byKeys(roots)
	for _, group := range children {
		byKeys(group)
	}

	out := results[:0]
	var emit func(group []*vcs.RepoStatus)
	emit = func(group []*vcs.RepoStatus) {
		for _, s := range group {
			out = append(out, s)
			emit(children[s.Path])
		}
	}
	emit(roots)
}

// Sort key implementations

type nameSortKey struct {
	reverse bool
}

func (k *nameSortKey) Compare(a, b *vcs.RepoStatus) int {
	nameA := filepath.Base(a.Path)
	nameB := filepath.Base(b.Path)

	result := 0
	if nameA < nameB {
		result = -1
	} else if nameA > nameB {
		result = 1
	}

	if k.reverse {
		result = -result
	}
	return result
}

type vcsSortKey struct {
	reverse bool
}

func (k *vcsSortKey) Compare(a, b *vcs.RepoStatus) int {
	// Sort order: Dir < Git < Jujutsu
	result := 0
	if a.Type < b.Type {
		result = -1
	} else if a.Type > b.Type {
		result = 1
	}

	if k.reverse {
		result = -result
	}
	return result
}

type dirtySortKey struct {
	reverse bool
}

func (k *dirtySortKey) Compare(a, b *vcs.RepoStatus) int {
	// Dirty repos come first (when not reversed)
	result := 0
	if a.Dirty && !b.Dirty {
		result = -1
	} else if !a.Dirty && b.Dirty {
		result = 1
	}

	if k.reverse {
		result = -result
	}
	return result
}

type aheadSortKey struct {
	reverse bool
}

func (k *aheadSortKey) Compare(a, b *vcs.RepoStatus) int {
	// Higher ahead counts come first; unknown counts sort as 0.
	result := 0
	if countValue(a.Ahead) > countValue(b.Ahead) {
		result = -1
	} else if countValue(a.Ahead) < countValue(b.Ahead) {
		result = 1
	}

	if k.reverse {
		result = -result
	}
	return result
}

// countValue maps a Count to its sort value; unknown counts sort as 0.
func countValue(c vcs.Count) int {
	if !c.Known {
		return 0
	}
	return c.N
}

type remoteSortKey struct {
	reverse bool
}

func (k *remoteSortKey) Compare(a, b *vcs.RepoStatus) int {
	// Repos with remotes come first (when not reversed)
	result := 0
	if a.Remote && !b.Remote {
		result = -1
	} else if !a.Remote && b.Remote {
		result = 1
	}

	if k.reverse {
		result = -result
	}
	return result
}

type behindSortKey struct {
	reverse bool
}

func (k *behindSortKey) Compare(a, b *vcs.RepoStatus) int {
	// Higher behind counts come first; unknown counts sort as 0.
	result := 0
	if countValue(a.Behind) > countValue(b.Behind) {
		result = -1
	} else if countValue(a.Behind) < countValue(b.Behind) {
		result = 1
	}

	if k.reverse {
		result = -result
	}
	return result
}
