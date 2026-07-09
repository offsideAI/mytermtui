package fsx

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// FindResult is one fuzzy-find hit.
type FindResult struct {
	Path  string // absolute
	Rel   string // relative to the search root, for display
	IsDir bool
	score int
}

// FuzzyFind walks root collecting entries whose relative path matches
// query as a case-insensitive subsequence. It scans at most limit
// entries and returns at most maxResults hits, best first.
func FuzzyFind(root, query string, limit, maxResults int, includeHidden bool) (results []FindResult, capped bool) {
	q := strings.ToLower(query)
	if q == "" {
		return nil, false
	}
	scanned := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if p == root {
			return nil
		}
		name := d.Name()
		if !includeHidden && strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() && (name == ".git" || name == "node_modules") {
			return filepath.SkipDir
		}
		scanned++
		if limit > 0 && scanned > limit {
			capped = true
			return filepath.SkipAll
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			rel = p
		}
		if sc, ok := fuzzyScore(strings.ToLower(rel), strings.ToLower(name), q); ok {
			results = append(results, FindResult{Path: p, Rel: rel, IsDir: d.IsDir(), score: sc})
		}
		return nil
	})
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return len(results[i].Rel) < len(results[j].Rel)
	})
	if maxResults > 0 && len(results) > maxResults {
		results = results[:maxResults]
	}
	return results, capped
}

// fuzzyScore matches q as a subsequence of rel (lowercased). Higher is
// better: basename hits and contiguous runs score up, path length down.
func fuzzyScore(rel, base, q string) (int, bool) {
	if strings.Contains(base, q) {
		return 1000 - len(rel), true
	}
	score := 0
	qi := 0
	streak := 0
	for i := 0; i < len(rel) && qi < len(q); i++ {
		if rel[i] == q[qi] {
			qi++
			streak++
			score += 1 + streak
		} else {
			streak = 0
		}
	}
	if qi < len(q) {
		return 0, false
	}
	return score - len(rel)/4, true
}
