package fsx

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"

	"golang.org/x/sys/unix"
)

// Progress is a shared counter a long-running operation updates and the
// UI polls from its tick loop.
type Progress struct {
	done  atomic.Int64
	total atomic.Int64
	label atomic.Value
}

func (p *Progress) Add(n int64)       { p.done.Add(n) }
func (p *Progress) AddTotal(n int64)  { p.total.Add(n) }
func (p *Progress) SetLabel(s string) { p.label.Store(s) }
func (p *Progress) Done() int64       { return p.done.Load() }
func (p *Progress) Total() int64      { return p.total.Load() }
func (p *Progress) Label() string {
	if v := p.label.Load(); v != nil {
		return v.(string)
	}
	return ""
}

// Undo reverses the last mutating operation (single level, best effort).
type Undo struct {
	Desc string
	Fn   func() error
}

// DuplicateName returns Finder-style "name copy.ext" (then "name copy 2.ext")
// that does not collide inside dir.
func DuplicateName(dir, name string) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		var cand string
		if i == 1 {
			cand = stem + " copy" + ext
		} else {
			cand = fmt.Sprintf("%s copy %d%s", stem, i, ext)
		}
		if _, err := os.Lstat(filepath.Join(dir, cand)); os.IsNotExist(err) {
			return cand
		}
	}
}

// KeepBothName returns "name 2.ext", "name 3.ext", … not colliding in dir.
func KeepBothName(dir, name string) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s %d%s", stem, i, ext)
		if _, err := os.Lstat(filepath.Join(dir, cand)); os.IsNotExist(err) {
			return cand
		}
	}
}

// TotalSize walks src summing regular-file sizes (for progress totals).
func TotalSize(src string) int64 {
	var n int64
	_ = filepath.WalkDir(src, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}

// CopyPath copies a file, symlink, or directory tree from src to dst
// (dst must not exist). On APFS it first attempts clonefile(2), which is
// instant, copy-on-write, and preserves everything including xattrs.
func CopyPath(src, dst string, prog *Progress) error {
	if err := clonefile(src, dst); err == nil {
		if prog != nil {
			prog.Add(TotalSize(dst))
		}
		return nil
	}
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case fi.Mode()&fs.ModeSymlink != 0:
		tgt, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(tgt, dst)
	case fi.IsDir():
		return copyDir(src, dst, fi, prog)
	default:
		return copyFile(src, dst, fi, prog)
	}
}

func copyDir(src, dst string, fi fs.FileInfo, prog *Progress) error {
	if err := os.Mkdir(dst, fi.Mode().Perm()|0o200); err != nil {
		return err
	}
	des, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, de := range des {
		if err := CopyPath(filepath.Join(src, de.Name()), filepath.Join(dst, de.Name()), prog); err != nil {
			return err
		}
	}
	copyXattrs(src, dst)
	_ = os.Chmod(dst, fi.Mode().Perm())
	_ = os.Chtimes(dst, fi.ModTime(), fi.ModTime())
	return nil
}

func copyFile(src, dst string, fi fs.FileInfo, prog *Progress) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fi.Mode().Perm())
	if err != nil {
		return err
	}
	buf := make([]byte, 1<<20)
	for {
		n, rerr := in.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				os.Remove(dst)
				return werr
			}
			if prog != nil {
				prog.Add(int64(n))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			os.Remove(dst)
			return rerr
		}
	}
	if err := out.Close(); err != nil {
		return err
	}
	copyXattrs(src, dst)
	_ = os.Chtimes(dst, fi.ModTime(), fi.ModTime())
	return nil
}

// copyXattrs is best-effort: losing an xattr should not fail the copy.
func copyXattrs(src, dst string) {
	sz, err := unix.Listxattr(src, nil)
	if err != nil || sz <= 0 {
		return
	}
	buf := make([]byte, sz)
	sz, err = unix.Listxattr(src, buf)
	if err != nil || sz <= 0 {
		return
	}
	for _, name := range strings.Split(strings.TrimRight(string(buf[:sz]), "\x00"), "\x00") {
		if name == "" {
			continue
		}
		vsz, err := unix.Getxattr(src, name, nil)
		if err != nil || vsz < 0 {
			continue
		}
		val := make([]byte, vsz)
		if vsz > 0 {
			if _, err = unix.Getxattr(src, name, val); err != nil {
				continue
			}
		}
		_ = unix.Setxattr(dst, name, val, 0)
	}
}

// Move renames src to dst, falling back to copy+delete across devices.
func Move(src, dst string, prog *Progress) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	var le *os.LinkError
	if errors.As(err, &le) && errors.Is(le.Err, syscall.EXDEV) {
		if err := CopyPath(src, dst, prog); err != nil {
			return err
		}
		return os.RemoveAll(src)
	}
	return err
}

// Zip compresses names (relative to dir) into destName inside dir.
// Single item: ditto (preserves resource forks/xattrs, like Finder).
// Multiple: zip -r.
func Zip(dir string, names []string, destName string) error {
	dest := filepath.Join(dir, destName)
	if _, err := os.Lstat(dest); err == nil {
		dest = filepath.Join(dir, KeepBothName(dir, destName))
	}
	var cmd *exec.Cmd
	if len(names) == 1 {
		cmd = exec.Command("ditto", "-ck", "--sequesterRsrc", "--keepParent", names[0], dest)
	} else {
		args := append([]string{"-r", "-q", dest}, names...)
		cmd = exec.Command("zip", args...)
	}
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}
