//go:build !darwin

package icloud

import "io/fs"

const sfDataless = 0x40000000

func flagsOf(fi fs.FileInfo) uint32 { return 0 }

func blocksOf(fi fs.FileInfo) int64 {
	if fi == nil {
		return 0
	}
	// Best effort: assume fully local on non-darwin.
	return (fi.Size() + 511) / 512
}

// FinderHidden is darwin-only; always false elsewhere.
func FinderHidden(fi fs.FileInfo) bool { return false }
