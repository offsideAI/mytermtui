package postgres

// Integration tests: set MYDB_TEST_PG_DSN to a reachable server, e.g.
//   MYDB_TEST_PG_DSN="host=localhost port=5432 user=postgres dbname=postgres"
// (password via the DSN or ~/.pgpass). Skipped when unset.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/offsideai/mydb/internal/dbx"
)

func testConn(t *testing.T) dbx.Conn {
	t.Helper()
	dsn := os.Getenv("MYDB_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("MYDB_TEST_PG_DSN not set")
	}
	pc, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg := dbx.ConnConfig{
		Host: pc.Host, Port: int(pc.Port), DBName: pc.Database,
		Username: pc.User, Password: pc.Password,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := New().Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// fixtureTable creates a throwaway table in a temp schema.
func fixtureTable(t *testing.T, c dbx.Conn) dbx.ObjectRef {
	t.Helper()
	pg := c.(*conn)
	ctx := context.Background()
	p, err := pg.poolFor(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("mydb_test_%d", time.Now().UnixNano())
	stmts := []string{
		"CREATE SCHEMA " + quoteIdent(schema),
		fmt.Sprintf(`CREATE TABLE %s.nums (id INT PRIMARY KEY, label TEXT NOT NULL, blob BYTEA)`, quoteIdent(schema)),
		fmt.Sprintf(`INSERT INTO %s.nums SELECT g, 'row-' || g, NULL FROM generate_series(1, 25) g`, quoteIdent(schema)),
		fmt.Sprintf(`CREATE INDEX idx_nums_label ON %s.nums (label)`, quoteIdent(schema)),
		fmt.Sprintf(`CREATE VIEW %s.evens AS SELECT id FROM %s.nums WHERE id %% 2 = 0`, quoteIdent(schema), quoteIdent(schema)),
	}
	for _, s := range stmts {
		if _, err := p.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	t.Cleanup(func() {
		p.Exec(context.Background(), "DROP SCHEMA "+quoteIdent(schema)+" CASCADE")
	})
	return dbx.ObjectRef{Schema: schema, Name: "nums"}
}

func TestServerInfoAndDatabases(t *testing.T) {
	c := testConn(t)
	ctx := context.Background()
	info, err := c.ServerInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(info) == 0 || info[0].Key != "engine" {
		t.Fatalf("server info: %+v", info)
	}
	dbs, err := c.Databases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(dbs) == 0 {
		t.Fatal("no databases listed")
	}
}

func TestIntrospection(t *testing.T) {
	c := testConn(t)
	ctx := context.Background()
	ref := fixtureTable(t, c)

	rels, err := c.Relations(ctx, "", ref.Schema)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 2 {
		t.Fatalf("want nums + evens, got %+v", rels)
	}

	cols, err := c.Columns(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 3 || cols[0].Name != "id" || !cols[0].PK || !cols[1].NotNull {
		t.Fatalf("columns: %+v", cols)
	}

	idxs, err := c.Indexes(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(idxs) != 2 { // pkey + idx_nums_label
		t.Fatalf("indexes: %+v", idxs)
	}

	roles, err := c.Roles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) == 0 {
		t.Fatal("no roles listed")
	}
}

func TestReadPageStable(t *testing.T) {
	c := testConn(t)
	ctx := context.Background()
	ref := fixtureTable(t, c)

	p1, err := c.ReadPage(ctx, ref, dbx.PageReq{Offset: 0, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(p1.Rows) != 10 || !p1.Truncated || p1.Rows[0][0].S != "1" {
		t.Fatalf("page 1: rows=%d truncated=%v first=%v", len(p1.Rows), p1.Truncated, p1.Rows[0])
	}
	p3, err := c.ReadPage(ctx, ref, dbx.PageReq{Offset: 20, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(p3.Rows) != 5 || p3.Truncated || p3.Rows[0][0].S != "21" {
		t.Fatalf("last page: rows=%d truncated=%v first=%v", len(p3.Rows), p3.Truncated, p3.Rows[0])
	}
	if p3.Rows[0][2].Kind != dbx.VNull {
		t.Errorf("NULL bytea cell: %+v", p3.Rows[0][2])
	}

	view := dbx.ObjectRef{Schema: ref.Schema, Name: "evens"}
	if _, err := c.ReadPage(ctx, view, dbx.PageReq{Limit: 5}); err != nil {
		t.Fatalf("view paging: %v", err)
	}
	if _, err := c.ReadPage(ctx, dbx.ObjectRef{Schema: ref.Schema, Name: "nope"}, dbx.PageReq{Limit: 5}); err == nil {
		t.Fatal("unknown relation must error")
	}
}
