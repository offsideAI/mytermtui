package icloud

// Live download progress. Non-entitled processes cannot observe
// fileproviderd transfers through public APIs: st_blocks stays 0 until
// the finished file swaps in, Spotlight hides ubiquitous attributes, and
// NSProgress file-progress subscriptions receive nothing (all verified
// empirically on macOS 26). The one accessible source is Apple's own
// `brctl status`, which is entitled and prints per-item downloader state
// including a percentage:
//
//	Under /some-folder/sub
//	    r:180272 … n:"O{80}M.mov" … sz:649.0 MB (648956754) …
//	    > downloader{[content:2yh1 downloading:34.0% op:… active …]}
//
// Names are elided as first-rune{middle-count}last-rune, so entries are
// matched to queue paths by directory, size, extension, and that
// first/last/length pattern. This file is the parser and matcher (pure,
// unit-tested); the darwin bridge runs the subprocess.

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type brctlEntry struct {
	dir  string // container-relative parent, e.g. "/docs/sub"
	name string // elided or literal file name as printed
	size int64  // exact byte size
	pct  float64
}

var (
	ansiRe       = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	underRe      = regexp.MustCompile(`^\s*Under (.+)$`)
	nameRe       = regexp.MustCompile(`n:"([^"]+)"`)
	sizeRe       = regexp.MustCompile(`\bsz:[^(]*\((\d+)\)`)
	downloadRe   = regexp.MustCompile(`downloading:([0-9.]+)%`)
	downloaderRe = regexp.MustCompile(`^\s*> downloader\{`)
	elidedRe     = regexp.MustCompile(`^(.)\{(\d+)\}(.)(\..*)?$`)
)

// parseBrctlStatus extracts per-item download percentages from
// `brctl status` output.
func parseBrctlStatus(out string) []brctlEntry {
	var entries []brctlEntry
	dir := ""
	var cur *brctlEntry
	for _, raw := range strings.Split(out, "\n") {
		line := ansiRe.ReplaceAllString(raw, "")
		if m := underRe.FindStringSubmatch(line); m != nil {
			dir = strings.TrimSpace(m[1])
			cur = nil
			continue
		}
		if downloaderRe.MatchString(line) {
			if cur != nil {
				if m := downloadRe.FindStringSubmatch(line); m != nil {
					if pct, err := strconv.ParseFloat(m[1], 64); err == nil {
						e := *cur
						e.pct = pct
						entries = append(entries, e)
					}
				}
			}
			continue
		}
		name := nameRe.FindStringSubmatch(line)
		size := sizeRe.FindStringSubmatch(line)
		if name != nil && size != nil {
			n, err := strconv.ParseInt(size[1], 10, 64)
			if err != nil {
				cur = nil
				continue
			}
			cur = &brctlEntry{dir: dir, name: name[1], size: n}
		}
	}
	return entries
}

// matchesName reports whether printed (possibly elided "A{75}y.mov")
// refers to base.
func matchesName(printed, base string) bool {
	if printed == base {
		return true
	}
	m := elidedRe.FindStringSubmatch(printed)
	if m == nil {
		return false
	}
	ext := m[4]
	if filepath.Ext(base) != ext {
		return false
	}
	stem := []rune(strings.TrimSuffix(base, ext))
	if len(stem) < 2 {
		return false
	}
	mid, err := strconv.Atoi(m[2])
	if err != nil {
		return false
	}
	return string(stem[0]) == m[1] &&
		string(stem[len(stem)-1]) == m[3] &&
		len(stem)-2 == mid
}

// progressFor finds the download percentage for path among parsed
// entries. size 0 skips the size check.
func progressFor(entries []brctlEntry, path string, size int64) (float64, bool) {
	base := filepath.Base(path)
	parent := filepath.Dir(path)
	root := DriveRoot()
	best, found := 0.0, false
	for _, e := range entries {
		if size != 0 && e.size != size {
			continue
		}
		if !matchesName(e.name, base) {
			continue
		}
		// Prefer a directory match; accept name+size otherwise.
		if e.dir != "" && filepath.Join(root, e.dir) == parent {
			return e.pct, true
		}
		best, found = e.pct, true
	}
	return best, found
}
