package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/offsideai/mydb/internal/dbx"
)

// fixture builds a small database with tables, a view, and indexes.
func fixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL UNIQUE, note TEXT DEFAULT 'hi')`,
		`CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id))`,
		`CREATE INDEX idx_orders_user ON orders(user_id)`,
		`CREATE VIEW user_emails AS SELECT email FROM users`,
		`INSERT INTO users (email) VALUES ('a@x.com'), ('b@x.com'), ('c@x.com')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	return path
}

func open(t *testing.T, path string) dbx.Conn {
	t.Helper()
	c, err := New().Open(context.Background(), dbx.ConnConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestOpenMissingFileFails(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.db")
	if _, err := New().Open(context.Background(), dbx.ConnConfig{Path: missing}); err == nil {
		t.Fatal("opening a missing file must fail, not create it")
	}
}

func TestOpenNonDatabaseFails(t *testing.T) {
	// A directory is the cheapest guaranteed non-database path.
	if _, err := New().Open(context.Background(), dbx.ConnConfig{Path: t.TempDir()}); err == nil {
		t.Fatal("opening a directory must fail")
	}
}

func TestRelations(t *testing.T) {
	c := open(t, fixture(t))
	rels, err := c.Relations(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]dbx.RelInfo{}
	for _, r := range rels {
		byName[r.Name] = r
	}
	if len(rels) != 3 {
		t.Fatalf("want 3 relations, got %d: %v", len(rels), byName)
	}
	if u := byName["users"]; u.Kind != dbx.RelTable || u.RowsEst != 3 {
		t.Errorf("users: want table with 3 rows, got %+v", u)
	}
	if v := byName["user_emails"]; v.Kind != dbx.RelView || v.RowsEst != -1 {
		t.Errorf("user_emails: want view with uncounted rows, got %+v", v)
	}
}

func TestColumns(t *testing.T) {
	c := open(t, fixture(t))
	cols, err := c.Columns(context.Background(), dbx.ObjectRef{Name: "users"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 3 {
		t.Fatalf("want 3 columns, got %d", len(cols))
	}
	if cols[0].Name != "id" || !cols[0].PK {
		t.Errorf("col 0: want PK id, got %+v", cols[0])
	}
	if cols[1].Name != "email" || !cols[1].NotNull || cols[1].TypeName != "TEXT" {
		t.Errorf("col 1: want NOT NULL TEXT email, got %+v", cols[1])
	}
	if cols[2].Default != "'hi'" {
		t.Errorf("col 2: want default 'hi', got %q", cols[2].Default)
	}
}

func TestIndexes(t *testing.T) {
	c := open(t, fixture(t))
	idxs, err := c.Indexes(context.Background(), dbx.ObjectRef{Name: "orders"})
	if err != nil {
		t.Fatal(err)
	}
	if len(idxs) != 1 {
		t.Fatalf("want 1 index, got %d: %+v", len(idxs), idxs)
	}
	if idxs[0].Name != "idx_orders_user" || idxs[0].Unique ||
		len(idxs[0].Columns) != 1 || idxs[0].Columns[0] != "user_id" {
		t.Errorf("unexpected index: %+v", idxs[0])
	}
}

func TestQuotedIdentifiers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quoted.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE "weird ""name" (x INTEGER)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	c := open(t, path)
	cols, err := c.Columns(context.Background(), dbx.ObjectRef{Name: `weird "name`})
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 1 || cols[0].Name != "x" {
		t.Errorf("want column x, got %+v", cols)
	}
}

func TestServerInfo(t *testing.T) {
	c := open(t, fixture(t))
	info, err := c.ServerInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(info) == 0 || info[0].Key != "engine" {
		t.Errorf("want engine first, got %+v", info)
	}
}
