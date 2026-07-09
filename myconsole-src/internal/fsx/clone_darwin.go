//go:build darwin

package fsx

import "golang.org/x/sys/unix"

// clonefile attempts an instant copy-on-write clone (APFS).
func clonefile(src, dst string) error {
	return unix.Clonefile(src, dst, unix.CLONE_NOFOLLOW)
}
