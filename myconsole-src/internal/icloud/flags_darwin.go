//go:build darwin

package icloud

import (
	"io/fs"
	"syscall"
)

// SF_DATALESS: file content is remote (FileProvider); reading materializes it.
const sfDataless = 0x40000000

// UF_HIDDEN: Finder-hidden flag.
const ufHidden = 0x00008000

func flagsOf(fi fs.FileInfo) uint32 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Flags
	}
	return 0
}

func blocksOf(fi fs.FileInfo) int64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Blocks
	}
	return 0
}

// FinderHidden reports the UF_HIDDEN flag.
func FinderHidden(fi fs.FileInfo) bool { return flagsOf(fi)&ufHidden != 0 }
