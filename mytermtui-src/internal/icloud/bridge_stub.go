//go:build !darwin || !cgo

package icloud

import (
	"os"
	"path/filepath"
	"strconv"
)

type stubBridge struct{}

// NewBridge returns a stub on platforms without the native bridge.
// Trash falls back to moving into ~/.Trash; download/evict are unsupported.
func NewBridge() Bridge { return stubBridge{} }

func (stubBridge) StartDownload(string) error { return ErrUnsupported }
func (stubBridge) Evict(string) error         { return ErrUnsupported }

func (stubBridge) DownloadProgress(string, int64) (float64, bool) { return 0, false }

func (stubBridge) Trash(path string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	trash := filepath.Join(home, ".Trash")
	if _, err := os.Stat(trash); err != nil {
		return "", ErrUnsupported
	}
	dst := filepath.Join(trash, filepath.Base(path))
	for i := 2; ; i++ {
		if _, err := os.Lstat(dst); os.IsNotExist(err) {
			break
		}
		ext := filepath.Ext(path)
		base := filepath.Base(path)
		dst = filepath.Join(trash, base[:len(base)-len(ext)]+" "+strconv.Itoa(i)+ext)
	}
	if err := os.Rename(path, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// WithNoMaterialize is a no-op guard on platforms without dataless files.
func WithNoMaterialize(fn func()) { fn() }
