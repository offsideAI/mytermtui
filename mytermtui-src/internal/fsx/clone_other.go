//go:build !darwin

package fsx

import "errors"

// clonefile is unsupported off macOS; callers fall back to a real copy.
func clonefile(src, dst string) error { return errors.New("clonefile unsupported") }
