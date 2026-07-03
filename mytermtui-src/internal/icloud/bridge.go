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
}

// ErrUnsupported is returned by the stub bridge on non-darwin platforms.
var ErrUnsupported = errors.New("iCloud operations require macOS")
