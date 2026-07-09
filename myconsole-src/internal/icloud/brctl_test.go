package icloud

import (
	"path/filepath"
	"testing"
)

// Synthetic sample modeled on real `brctl status` output (ANSI colors,
// elided names, exact sizes in parens, downloader lines with percent).
const brctlSample = "2 containers matching 'com.apple.CloudDocs'\n" +
	"<container header noise>\n" +
	"    Client Truth Unclean Items:\n" +
	"    --------------------------\n" +
	"    Under /demo-docs/recordings\n" +
	"        r:1 i:\x1b[33m<AAAA>\x1b[39m st{n:\"\x1b[0;1md{16}1.mov\x1b[0m\" doc} ct{etag:x mt:1 sz:\x1b[0;1;30m2.99 GB (2987460592)\x1b[0m n:\"d{16}1.mov\" device:5}\n" +
	"        > downloader{[content:x downloading:34.0% op:UUID \x1b[0;1;32mactive\x1b[0m attempts:0]}\n" +
	"        r:2 i:<BBBB> st{n:\"d{16}2.mov\" doc} ct{etag:y mt:1 sz:649.0 MB (648956754) n:\"d{16}2.mov\"}\n" +
	"        > downloader{[content:y \x1b[0;1;32mactive\x1b[0m attempts:0]}\n" +
	"    Under /other\n" +
	"        r:3 i:<CCCC> st{n:\"notes.txt\" doc} ct{etag:z mt:1 sz:1.0 KB (1024) n:\"notes.txt\"}\n" +
	"        > downloader{[content:z downloading:80.5% active]}\n"

func TestParseBrctlStatus(t *testing.T) {
	entries := parseBrctlStatus(brctlSample)
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2 (only downloader lines with percent)", len(entries))
	}
	if entries[0].dir != "/demo-docs/recordings" || entries[0].size != 2987460592 || entries[0].pct != 34.0 {
		t.Errorf("entry 0 = %+v", entries[0])
	}
	if entries[1].dir != "/other" || entries[1].name != "notes.txt" || entries[1].pct != 80.5 {
		t.Errorf("entry 1 = %+v", entries[1])
	}
}

func TestMatchesName(t *testing.T) {
	cases := []struct {
		printed, base string
		want          bool
	}{
		{"notes.txt", "notes.txt", true},
		{"d{16}1.mov", "demo-recording-2-1.mov", true},  // d + 16 runes + 1
		{"d{16}1.mov", "demo-recording-2-2.mov", false}, // last rune differs
		{"d{15}1.mov", "demo-recording-2-1.mov", false}, // middle length differs
		{"d{16}1.mov", "demo-recording-2-1.mp4", false}, // extension differs
		{"x{3}y.mov", "xაბგy.mov", true},                // rune counting, not bytes
	}
	for _, c := range cases {
		if got := matchesName(c.printed, c.base); got != c.want {
			t.Errorf("matchesName(%q, %q) = %v, want %v", c.printed, c.base, got, c.want)
		}
	}
}

func TestProgressFor(t *testing.T) {
	entries := parseBrctlStatus(brctlSample)
	dir := filepath.Join(DriveRoot(), "demo-docs", "recordings")

	// d{16}1.mov, 2987460592 bytes → 34%
	pct, ok := progressFor(entries, filepath.Join(dir, "demo-recording-2-1.mov"), 2987460592)
	if !ok || pct != 34.0 {
		t.Fatalf("pct = %v ok=%v, want 34", pct, ok)
	}
	// size mismatch → no match
	if _, ok := progressFor(entries, filepath.Join(dir, "demo-recording-2-1.mov"), 123); ok {
		t.Fatal("matched despite wrong size")
	}
	// name mismatch → no match
	if _, ok := progressFor(entries, filepath.Join(dir, "zzz.mov"), 2987460592); ok {
		t.Fatal("matched despite wrong name")
	}
	// literal name in another dir, size skipped
	pct, ok = progressFor(entries, filepath.Join(DriveRoot(), "other", "notes.txt"), 0)
	if !ok || pct != 80.5 {
		t.Fatalf("notes pct = %v ok=%v", pct, ok)
	}
}
