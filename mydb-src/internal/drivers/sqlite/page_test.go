package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/offsideai/mydb/internal/dbx"
)

// pagingFixture: 25 rows, plus a WITHOUT ROWID table and a blob/null mix.
func pagingFixture(t *testing.T) dbx.Conn {
	t.Helper()
	path := filepath.Join(t.TempDir(), "page.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	stmts := []string{
		`CREATE TABLE nums (id INTEGER PRIMARY KEY, label TEXT)`,
		`CREATE TABLE kv (k TEXT NOT NULL, v BLOB, PRIMARY KEY (k)) WITHOUT ROWID`,
		`CREATE TABLE bare (x TEXT)`, // no PK → rowid ordering
		`CREATE VIEW evens AS SELECT id, label FROM nums WHERE id % 2 = 0`,
		`INSERT INTO kv VALUES ('a', x'deadbeef'), ('b', NULL)`,
		`INSERT INTO bare VALUES ('z'), ('y')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	for i := 1; i <= 25; i++ {
		if _, err := db.Exec(`INSERT INTO nums (id, label) VALUES (?, ?)`, i, fmt.Sprintf("row-%02d", i)); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	c, err := New().Open(context.Background(), dbx.ConnConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestReadPageStableAcrossPages(t *testing.T) {
	c := pagingFixture(t)
	ref := dbx.ObjectRef{Name: "nums"}

	p1, err := c.ReadPage(context.Background(), ref, dbx.PageReq{Offset: 0, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(p1.Rows) != 10 || !p1.Truncated {
		t.Fatalf("page 1: %d rows, truncated=%v", len(p1.Rows), p1.Truncated)
	}
	if p1.Columns[0].Name != "id" || p1.Rows[0][0].S != "1" || p1.Rows[9][0].S != "10" {
		t.Fatalf("page 1 order wrong: first=%v last=%v", p1.Rows[0][0], p1.Rows[9][0])
	}

	p3, err := c.ReadPage(context.Background(), ref, dbx.PageReq{Offset: 20, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(p3.Rows) != 5 || p3.Truncated {
		t.Fatalf("last page: %d rows, truncated=%v", len(p3.Rows), p3.Truncated)
	}
	if p3.Rows[0][0].S != "21" || p3.Rows[4][1].S != "row-25" {
		t.Fatalf("last page order wrong: %v", p3.Rows)
	}
}

func TestReadPageWithoutRowid(t *testing.T) {
	c := pagingFixture(t)
	p, err := c.ReadPage(context.Background(), dbx.ObjectRef{Name: "kv"}, dbx.PageReq{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(p.Rows))
	}
	if p.Rows[0][1].Kind != dbx.VBytes || p.Rows[0][1].S != "0xdeadbeef" {
		t.Errorf("blob cell: %+v", p.Rows[0][1])
	}
	if p.Rows[1][1].Kind != dbx.VNull {
		t.Errorf("null cell: %+v", p.Rows[1][1])
	}
}

func TestReadPageBareTableAndView(t *testing.T) {
	c := pagingFixture(t)
	if _, err := c.ReadPage(context.Background(), dbx.ObjectRef{Name: "bare"}, dbx.PageReq{Limit: 5}); err != nil {
		t.Fatalf("bare table (rowid order): %v", err)
	}
	p, err := c.ReadPage(context.Background(), dbx.ObjectRef{Name: "evens"}, dbx.PageReq{Limit: 5})
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if len(p.Rows) != 5 || !p.Truncated {
		t.Fatalf("view page: %d rows truncated=%v", len(p.Rows), p.Truncated)
	}
}

func TestReadPageUnknownRelation(t *testing.T) {
	c := pagingFixture(t)
	if _, err := c.ReadPage(context.Background(), dbx.ObjectRef{Name: "nope"}, dbx.PageReq{Limit: 5}); err == nil {
		t.Fatal("unknown relation must error, not run raw SQL")
	}
}
