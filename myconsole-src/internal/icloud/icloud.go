// Package icloud detects iCloud Drive dataless ("evicted") files and
// drives downloads/evictions through a native bridge.
//
// On modern macOS (FileProvider-based iCloud Drive) an evicted file is a
// *dataless* file: it keeps its name and logical size but occupies zero
// blocks and carries the SF_DATALESS stat flag. There are no .icloud
// placeholder files and no brctl download/evict CLI anymore.
package icloud

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Status of a path relative to iCloud.
type Status int

const (
	StatusNotICloud Status = iota // outside any iCloud container
	StatusLocal                   // in iCloud, fully materialized
	StatusEvicted                 // in iCloud, dataless (cloud-only)
)

// MobileDocs is the root of all iCloud containers for the current user.
func MobileDocs() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Mobile Documents")
}

// DriveRoot is the iCloud Drive documents container.
func DriveRoot() string {
	return filepath.Join(MobileDocs(), "com~apple~CloudDocs")
}

// InICloud reports whether path lies inside ~/Library/Mobile Documents.
func InICloud(path string) bool {
	root := MobileDocs()
	if root == "" {
		return false
	}
	return path == root || strings.HasPrefix(path, root+string(os.PathSeparator))
}

// Dataless reports whether the stat carries SF_DATALESS.
func Dataless(fi fs.FileInfo) bool { return flagsOf(fi)&sfDataless != 0 }

// LocalBytes is the number of bytes actually present on disk.
func LocalBytes(fi fs.FileInfo) int64 { return blocksOf(fi) * 512 }

// StatusOf classifies a path. The FileInfo must come from Lstat.
func StatusOf(path string, fi fs.FileInfo) Status {
	if !InICloud(path) {
		return StatusNotICloud
	}
	if Dataless(fi) {
		return StatusEvicted
	}
	return StatusLocal
}

// ExpandDataless walks root and returns every dataless regular file under
// it (or root itself, if root is a dataless file). Walking reads directory
// listings only — it never reads file contents, so nothing is downloaded.
// The walk stops after limit entries scanned (0 = no limit); the bool
// result reports whether the walk was capped.
func ExpandDataless(root string, limit int) ([]string, bool, error) {
	fi, err := os.Lstat(root)
	if err != nil {
		return nil, false, err
	}
	if !fi.IsDir() {
		if Dataless(fi) {
			return []string{root}, false, nil
		}
		return nil, false, nil
	}
	var out []string
	scanned := 0
	capped := false
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable subtree: skip silently
		}
		scanned++
		if limit > 0 && scanned > limit {
			capped = true
			return filepath.SkipAll
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil && Dataless(info) {
				out = append(out, p)
			}
		}
		return nil
	})
	return out, capped, err
}

// Summary aggregates local vs cloud-only usage under a directory.
type Summary struct {
	LocalFiles int
	CloudFiles int
	LocalBytes int64
	CloudBytes int64
}

// Summarize walks root (listings only) and tallies materialized vs
// evicted files. Capped at limit entries (0 = no limit).
func Summarize(root string, limit int) (Summary, bool, error) {
	var s Summary
	scanned := 0
	capped := false
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		scanned++
		if limit > 0 && scanned > limit {
			capped = true
			return filepath.SkipAll
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if Dataless(info) {
			s.CloudFiles++
			s.CloudBytes += info.Size()
		} else {
			s.LocalFiles++
			s.LocalBytes += info.Size()
		}
		return nil
	})
	return s, capped, err
}
