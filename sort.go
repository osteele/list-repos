package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// SortKey represents a field to sort by
type SortKey interface {
	Compare(a, b *RepoStatus) int
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

// SortResults sorts repository statuses according to the given sort keys
func SortResults(results []*RepoStatus, keys []SortKey) {
	sort.Slice(results, func(i, j int) bool {
		for _, key := range keys {
			cmp := key.Compare(results[i], results[j])
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

// Sort key implementations

type nameSortKey struct {
	reverse bool
}

func (k *nameSortKey) Compare(a, b *RepoStatus) int {
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

func (k *vcsSortKey) Compare(a, b *RepoStatus) int {
	// Sort order: Git < Jujutsu < Bare
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

func (k *dirtySortKey) Compare(a, b *RepoStatus) int {
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

func (k *aheadSortKey) Compare(a, b *RepoStatus) int {
	// Repos with ahead commits come first (when not reversed)
	result := 0
	if a.Ahead && !b.Ahead {
		result = -1
	} else if !a.Ahead && b.Ahead {
		result = 1
	}

	if k.reverse {
		result = -result
	}
	return result
}

type remoteSortKey struct {
	reverse bool
}

func (k *remoteSortKey) Compare(a, b *RepoStatus) int {
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
