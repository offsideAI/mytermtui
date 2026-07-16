package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/offsideai/mydb/internal/dbx"
)

func emptyDB(t *testing.T) dbx.Conn {
	t.Helper()
	path := filepath.Join(t.TempDir(), "q.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE seed (x INTEGER)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	c, err := New().Open(context.Background(), dbx.ConnConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestQueryScript(t *testing.T) {
	c := emptyDB(t)
	res, err := c.Query(context.Background(), "", `
		CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT);
		INSERT INTO t (name) VALUES ('a'), ('b'), ('c');
		UPDATE t SET name = 'z' WHERE id = 1;
		SELECT id, name FROM t ORDER BY id;
	`, dbx.QueryReq{MaxRows: 100})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stmts != 4 {
		t.Errorf("stmts = %d, want 4", res.Stmts)
	}
	if res.Affected != 4 { // 3 inserts + 1 update
		t.Errorf("affected = %d, want 4", res.Affected)
	}
	if len(res.Rows) != 3 || res.Rows[0][1].S != "z" {
		t.Errorf("last result set wrong: %+v", res.Rows)
	}
}

func TestQueryTruncation(t *testing.T) {
	c := emptyDB(t)
	res, err := c.Query(context.Background(), "",
		"WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x < 50) SELECT x FROM n",
		dbx.QueryReq{MaxRows: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 10 || !res.Truncated {
		t.Fatalf("rows=%d truncated=%v", len(res.Rows), res.Truncated)
	}
}

func TestQueryError(t *testing.T) {
	c := emptyDB(t)
	if _, err := c.Query(context.Background(), "", "SELECT * FROM nope", dbx.QueryReq{}); err == nil {
		t.Fatal("bad SQL must error")
	}
}

func TestQueryCancel(t *testing.T) {
	c := emptyDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Query(ctx, "",
		"WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n) SELECT count(*) FROM n",
		dbx.QueryReq{})
	if err == nil {
		t.Fatal("infinite query should be interrupted")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("cancellation took %v — interrupt not working", time.Since(start))
	}
}

func TestExplainCapability(t *testing.T) {
	caps := New().Capabilities()
	if caps.Explain == "" || caps.ExplainAnalyze != "" {
		t.Fatalf("sqlite explain caps wrong: %+v", caps)
	}
	c := emptyDB(t)
	res, err := c.Query(context.Background(), "", caps.Explain+"SELECT * FROM seed", dbx.QueryReq{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) == 0 {
		t.Fatal("EXPLAIN QUERY PLAN should return rows")
	}
}
