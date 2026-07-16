package jobs

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func makeSQLiteDB(t *testing.T, rows int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "src.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < rows; i++ {
		if _, err := db.Exec("INSERT INTO t (v) VALUES (?)", "row"); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestSQLiteBackupProducesUsableCopy(t *testing.T) {
	src := makeSQLiteDB(t, 100)
	dest := filepath.Join(t.TempDir(), "backup.db")

	run := SQLiteBackup(src, dest)
	if err := run(context.Background(), func(int64, int64, string) {}); err != nil {
		t.Fatal(err)
	}

	// The copy opens and has the data.
	db, err := sql.Open("sqlite", "file:"+dest+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM t").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 100 {
		t.Fatalf("backup has %d rows, want 100", n)
	}
}

func TestSQLiteBackupReportsProgress(t *testing.T) {
	src := makeSQLiteDB(t, 100)
	dest := filepath.Join(t.TempDir(), "b.db")
	var lastTotal int64 = -2
	run := SQLiteBackup(src, dest)
	if err := run(context.Background(), func(done, total int64, phase string) {
		lastTotal = total
	}); err != nil {
		t.Fatal(err)
	}
	if lastTotal <= 0 {
		t.Fatalf("expected a known total size, got %d", lastTotal)
	}
}

func TestSQLiteBackupRefusesExistingDest(t *testing.T) {
	src := makeSQLiteDB(t, 1)
	dest := filepath.Join(t.TempDir(), "exists.db")
	os.WriteFile(dest, []byte("x"), 0o644)
	if err := SQLiteBackup(src, dest)(context.Background(), func(int64, int64, string) {}); err == nil {
		t.Fatal("must refuse to overwrite an existing file")
	}
}

func TestSQLiteIntegrityCheck(t *testing.T) {
	src := makeSQLiteDB(t, 5)
	if err := SQLiteIntegrityCheck(src); err != nil {
		t.Fatalf("clean db failed integrity check: %v", err)
	}
}

func TestToolProbe(t *testing.T) {
	// `go` is always on PATH in CI; a bogus name never is.
	if _, ok := Tool("definitely-not-a-real-binary-xyz"); ok {
		t.Error("missing tool reported present")
	}
}

func TestPGConnArgs(t *testing.T) {
	c := PGConn{Host: "h", Port: 5432, DBName: "db", Username: "u", Password: "p"}
	args := strings.Join(c.args(), " ")
	if args != "-h h -p 5432 -U u" {
		t.Errorf("args = %q", args)
	}
	found := false
	for _, e := range c.env() {
		if e == "PGPASSWORD=p" {
			found = true
		}
	}
	if !found {
		t.Error("PGPASSWORD not in env")
	}
}
