package fsx

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadDirAndSort(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "beta.txt"), "22")
	write(t, filepath.Join(dir, "Alpha.txt"), "1")
	write(t, filepath.Join(dir, ".hidden"), "")
	if err := os.Mkdir(filepath.Join(dir, "zdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	entries, err := ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("got %d entries, want 4 (hidden included)", len(entries))
	}

	Sort(entries, SortName, true, true)
	if entries[0].Name != "zdir" {
		t.Errorf("dirs-first: first = %q, want zdir", entries[0].Name)
	}
	if entries[1].Name != ".hidden" || entries[2].Name != "Alpha.txt" || entries[3].Name != "beta.txt" {
		t.Errorf("name sort order = %q %q %q", entries[1].Name, entries[2].Name, entries[3].Name)
	}

	// Note: dirs have a nonzero st_size on APFS, so keep dirs-first on
	// and assert the file ordering below the directory.
	Sort(entries, SortSize, false, true)
	if entries[0].Name != "zdir" || entries[1].Name != "beta.txt" {
		t.Errorf("size desc: order = %q, %q; want zdir, beta.txt", entries[0].Name, entries[1].Name)
	}

	hidden := 0
	for _, e := range entries {
		if e.Hidden {
			hidden++
		}
	}
	if hidden != 1 {
		t.Errorf("hidden count = %d, want 1", hidden)
	}
}

func TestSortModified(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.txt")
	newer := filepath.Join(dir, "new.txt")
	write(t, old, "o")
	write(t, newer, "n")
	past := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	entries, err := ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	Sort(entries, SortModified, false, true)
	if entries[0].Name != "new.txt" {
		t.Errorf("modified desc: first = %q, want new.txt", entries[0].Name)
	}
}

func TestNames(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "report.pdf"), "x")
	write(t, filepath.Join(dir, "report copy.pdf"), "x")
	write(t, filepath.Join(dir, "report 2.pdf"), "x")

	if got := DuplicateName(dir, "report.pdf"); got != "report copy 2.pdf" {
		t.Errorf("DuplicateName = %q, want report copy 2.pdf", got)
	}
	if got := KeepBothName(dir, "report.pdf"); got != "report 3.pdf" {
		t.Errorf("KeepBothName = %q, want report 3.pdf", got)
	}
	if got := DuplicateName(dir, "fresh.txt"); got != "fresh copy.txt" {
		t.Errorf("DuplicateName fresh = %q", got)
	}
}

func TestCopyPathFileAndTree(t *testing.T) {
	src := t.TempDir()
	dstRoot := t.TempDir()
	write(t, filepath.Join(src, "f.txt"), "content")
	write(t, filepath.Join(src, "sub", "g.txt"), "nested")
	if err := os.Symlink("f.txt", filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}

	var prog Progress
	dst := filepath.Join(dstRoot, "copy")
	if err := CopyPath(src, dst, &prog); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dst, "sub", "g.txt"))
	if err != nil || string(got) != "nested" {
		t.Errorf("nested copy = %q err=%v", got, err)
	}
	tgt, err := os.Readlink(filepath.Join(dst, "link"))
	if err != nil || tgt != "f.txt" {
		t.Errorf("symlink copy = %q err=%v", tgt, err)
	}
	if prog.Done() <= 0 {
		t.Errorf("progress done = %d, want > 0", prog.Done())
	}
}

func TestMoveSameVolume(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	dst := filepath.Join(dir, "b.txt")
	write(t, src, "move me")
	if err := Move(src, dst, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(src); !os.IsNotExist(err) {
		t.Error("source still exists after move")
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "move me" {
		t.Errorf("moved content = %q", got)
	}
}

func TestTotalSize(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a"), "12345")
	write(t, filepath.Join(dir, "sub", "b"), "123")
	if got := TotalSize(dir); got != 8 {
		t.Errorf("TotalSize = %d, want 8", got)
	}
}

func TestFuzzyFind(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "docs", "Demo-Walkthrough.mov"), "x")
	write(t, filepath.Join(dir, "docs", "other.txt"), "x")
	write(t, filepath.Join(dir, ".git", "config"), "x")

	results, capped := FuzzyFind(dir, "walk", 0, 50, false)
	if capped {
		t.Error("unexpected cap")
	}
	if len(results) != 1 || filepath.Base(results[0].Path) != "Demo-Walkthrough.mov" {
		t.Fatalf("results = %+v, want the walkthrough", results)
	}

	// subsequence across path segments
	results, _ = FuzzyFind(dir, "docsother", 0, 50, false)
	if len(results) != 1 || filepath.Base(results[0].Path) != "other.txt" {
		t.Errorf("subsequence results = %+v", results)
	}

	// hidden dirs are skipped
	results, _ = FuzzyFind(dir, "config", 0, 50, false)
	if len(results) != 0 {
		t.Errorf("hidden results = %+v, want none", results)
	}
}

func TestEntryKindAndClass(t *testing.T) {
	e := Entry{Name: "movie.MOV"}
	if e.Kind() != "QuickTime movie" {
		t.Errorf("Kind = %q", e.Kind())
	}
	if e.Class() != ClassMedia {
		t.Errorf("Class = %v, want media", e.Class())
	}
	d := Entry{Name: "stuff", IsDir: true}
	if d.Kind() != "folder" || d.Class() != ClassDir {
		t.Errorf("dir kind/class = %q %v", d.Kind(), d.Class())
	}
}
