package icloud

import "errors"

// Bridge abstracts the native calls so the queue is testable and the
// package compiles on non-darwin platforms.
type Bridge interface {
	// StartDownload asks fileproviderd to materialize the item (async).
	StartDownload(path string) error
	// Evict removes the local copy, keeping the item in the cloud.
	Evict(path string) error
	// Trash moves the item to the user's Trash with Finder put-back
	// semantics, returning the item's new location inside the Trash.
	Trash(path string) (putback string, err error)
	// DownloadProgress reports the percent downloaded (0–100) for a
	// file currently being materialized. fileproviderd stages downloads
	// out of view (st_blocks stays 0 until completion) and hides its
	// progress from non-entitled processes, so the darwin bridge polls
	// Apple's entitled `brctl status` and matches items by name pattern
	// and size — see brctl.go.
	DownloadProgress(path string, size int64) (pct float64, ok bool)
}

// ErrUnsupported is returned by the stub bridge on non-darwin platforms.
var ErrUnsupported = errors.New("iCloud operations require macOS")
