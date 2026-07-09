package icloud

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInICloud(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	root := filepath.Join(home, "Library", "Mobile Documents")
	cases := []struct {
		path string
		want bool
	}{
		{root, true},
		{filepath.Join(root, "com~apple~CloudDocs"), true},
		{filepath.Join(root, "com~apple~CloudDocs", "deep", "file.mov"), true},
		{home, false},
		{"/tmp/whatever", false},
		{filepath.Join(home, "Library", "Mobile Documentsx"), false},
	}
	for _, c := range cases {
		if got := InICloud(c.path); got != c.want {
			t.Errorf("InICloud(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestDatalessOnRegularFile(t *testing.T) {
	// A freshly written temp file must never be dataless.
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	if Dataless(fi) {
		t.Error("temp file reported dataless")
	}
	if LocalBytes(fi) <= 0 {
		t.Errorf("LocalBytes = %d, want > 0", LocalBytes(fi))
	}
}

func TestExpandDatalessOnLocalTree(t *testing.T) {
	// No dataless files in a local tree → empty result, not an error.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, capped, err := ExpandDataless(dir, 0)
	if err != nil || capped || len(files) != 0 {
		t.Errorf("ExpandDataless = %v capped=%v err=%v, want empty", files, capped, err)
	}
}

func TestSummarizeCountsLocal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), make([]byte, 50), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, capped, err := Summarize(dir, 0)
	if err != nil || capped {
		t.Fatalf("Summarize err=%v capped=%v", err, capped)
	}
	if sum.LocalFiles != 2 || sum.LocalBytes != 150 || sum.CloudFiles != 0 {
		t.Errorf("Summarize = %+v", sum)
	}
}
