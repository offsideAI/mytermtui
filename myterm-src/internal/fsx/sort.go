package fsx

import (
	"sort"
	"strings"
)

// SortBy selects the listing sort key.
type SortBy int

const (
	SortName SortBy = iota
	SortSize
	SortModified
	SortKind
)

func (s SortBy) String() string {
	switch s {
	case SortName:
		return "name"
	case SortSize:
		return "size"
	case SortModified:
		return "modified"
	case SortKind:
		return "kind"
	}
	return "?"
}

// Sort orders entries in place.
func Sort(entries []Entry, by SortBy, asc, dirsFirst bool) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if dirsFirst && a.IsDir != b.IsDir {
			return a.IsDir
		}
		less := false
		switch by {
		case SortSize:
			if a.Size != b.Size {
				less = a.Size < b.Size
			} else {
				less = nameLess(a.Name, b.Name)
			}
		case SortModified:
			if !a.ModTime.Equal(b.ModTime) {
				less = a.ModTime.Before(b.ModTime)
			} else {
				less = nameLess(a.Name, b.Name)
			}
		case SortKind:
			ka, kb := a.Kind(), b.Kind()
			if ka != kb {
				less = ka < kb
			} else {
				less = nameLess(a.Name, b.Name)
			}
		default:
			less = nameLess(a.Name, b.Name)
		}
		if !asc {
			return !less && !equalKey(a, b, by)
		}
		return less
	})
}

func nameLess(a, b string) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la != lb {
		return la < lb
	}
	return a < b
}

func equalKey(a, b Entry, by SortBy) bool {
	switch by {
	case SortSize:
		return a.Size == b.Size && a.Name == b.Name
	case SortModified:
		return a.ModTime.Equal(b.ModTime) && a.Name == b.Name
	case SortKind:
		return a.Kind() == b.Kind() && a.Name == b.Name
	default:
		return a.Name == b.Name
	}
}
