// Package fsx provides directory listing and file operations for the UI.
// All paths are treated as opaque byte strings: iCloud screen recordings
// and friends contain characters like U+202F, so nothing here ever
// round-trips a name through a shell.
package fsx

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/offsideai/myconsole/internal/icloud"
)

// Entry is one row of a directory listing.
type Entry struct {
	Name       string
	Path       string
	IsDir      bool
	IsLink     bool
	LinkBroken bool
	LinkTarget string
	Size       int64
	LocalBytes int64
	Mode       fs.FileMode
	ModTime    time.Time
	Hidden     bool
	Dataless   bool
	InICloud   bool
	StatErr    string
	Depth      int // tree indentation level in the flattened listing
}

// Status classifies the entry for the iCloud glyph column.
func (e Entry) Status() icloud.Status {
	if !e.InICloud {
		return icloud.StatusNotICloud
	}
	if e.Dataless {
		return icloud.StatusEvicted
	}
	return icloud.StatusLocal
}

// ReadDir lists path. Hidden entries are included (the UI filters), so a
// visibility toggle needs no re-read.
func ReadDir(path string) ([]Entry, error) {
	des, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	inCloud := icloud.InICloud(path)
	entries := make([]Entry, 0, len(des))
	for _, de := range des {
		e := Entry{
			Name:     de.Name(),
			Path:     filepath.Join(path, de.Name()),
			IsDir:    de.IsDir(),
			IsLink:   de.Type()&fs.ModeSymlink != 0,
			Hidden:   strings.HasPrefix(de.Name(), "."),
			InICloud: inCloud,
		}
		fi, err := de.Info()
		if err != nil {
			e.StatErr = err.Error()
			entries = append(entries, e)
			continue
		}
		e.Size = fi.Size()
		e.Mode = fi.Mode()
		e.ModTime = fi.ModTime()
		e.LocalBytes = icloud.LocalBytes(fi)
		e.Dataless = icloud.Dataless(fi)
		if !e.Hidden {
			e.Hidden = icloud.FinderHidden(fi)
		}
		if e.IsLink {
			if tgt, err := os.Readlink(e.Path); err == nil {
				e.LinkTarget = tgt
				if st, err := os.Stat(e.Path); err != nil {
					e.LinkBroken = true
				} else {
					e.IsDir = st.IsDir()
				}
			} else {
				e.LinkBroken = true
			}
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// Kind returns a short human label for the info panel and Kind sorting.
func (e Entry) Kind() string {
	switch {
	case e.IsLink && e.LinkBroken:
		return "broken symlink"
	case e.IsLink:
		return "symlink"
	case e.IsDir && strings.HasSuffix(e.Name, ".app"):
		return "application"
	case e.IsDir:
		return "folder"
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(e.Name), "."))
	if ext == "" {
		if e.Mode&0o111 != 0 {
			return "executable"
		}
		return "document"
	}
	if k, ok := kindNames[ext]; ok {
		return k
	}
	return ext + " file"
}

var kindNames = map[string]string{
	"mov": "QuickTime movie", "mp4": "MPEG-4 movie", "mkv": "Matroska movie",
	"m4v": "movie", "avi": "AVI movie", "webm": "WebM movie",
	"mp3": "MP3 audio", "m4a": "audio", "wav": "waveform audio", "flac": "FLAC audio",
	"aiff": "AIFF audio", "ogg": "Ogg audio",
	"png": "PNG image", "jpg": "JPEG image", "jpeg": "JPEG image", "gif": "GIF image",
	"heic": "HEIC image", "webp": "WebP image", "svg": "SVG image", "tiff": "TIFF image",
	"pdf": "PDF document", "txt": "plain text", "md": "Markdown text",
	"zip": "ZIP archive", "gz": "gzip archive", "tar": "tar archive", "dmg": "disk image",
	"go": "Go source", "py": "Python source", "js": "JavaScript source",
	"ts": "TypeScript source", "c": "C source", "h": "C header", "swift": "Swift source",
	"json": "JSON data", "toml": "TOML data", "yaml": "YAML data", "yml": "YAML data",
	"log": "log file", "app": "application",
}

// KindClass buckets entries for row coloring.
type KindClass int

const (
	ClassFile KindClass = iota
	ClassDir
	ClassLink
	ClassBrokenLink
	ClassExec
	ClassMedia
	ClassImage
	ClassArchive
)

var mediaExts = map[string]bool{
	"mov": true, "mp4": true, "mkv": true, "m4v": true, "avi": true, "webm": true,
	"mp3": true, "m4a": true, "wav": true, "flac": true, "aiff": true, "ogg": true,
}
var imageExts = map[string]bool{
	"png": true, "jpg": true, "jpeg": true, "gif": true, "heic": true,
	"webp": true, "svg": true, "tiff": true, "bmp": true, "icns": true,
}
var archiveExts = map[string]bool{
	"zip": true, "gz": true, "tgz": true, "tar": true, "bz2": true,
	"xz": true, "7z": true, "rar": true, "dmg": true,
}

// Class returns the color bucket for the entry.
func (e Entry) Class() KindClass {
	switch {
	case e.IsLink && e.LinkBroken:
		return ClassBrokenLink
	case e.IsLink:
		return ClassLink
	case e.IsDir:
		return ClassDir
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(e.Name), "."))
	switch {
	case mediaExts[ext]:
		return ClassMedia
	case imageExts[ext]:
		return ClassImage
	case archiveExts[ext]:
		return ClassArchive
	case e.Mode&0o111 != 0:
		return ClassExec
	}
	return ClassFile
}
