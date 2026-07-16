// Package sqlite implements dbx.Driver over modernc.org/sqlite (pure
// Go). Introspection uses sqlite_master and PRAGMAs.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/offsideai/mydb/internal/dbx"
)

// rowsEstCap bounds COUNT(*) work on huge tables; counts above the cap
// report cap+1 and render as "cap+".
const rowsEstCap = 1_000_000

type Driver struct{}

func New() Driver { return Driver{} }

func (Driver) Engine() string { return "sqlite" }

func (Driver) Capabilities() dbx.Capabilities {
	return dbx.Capabilities{
		TransactionalDDL: true,
		ServerCancel:     true, // context cancellation → sqlite3_interrupt
	}
}

// Open connects to the SQLite file at cfg.Path. The file must already
// exist: a browser must never silently create an empty database at a
// mistyped path.
func (Driver) Open(ctx context.Context, cfg dbx.ConnConfig) (dbx.Conn, error) {
	fi, err := os.Stat(cfg.Path)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return nil, fmt.Errorf("%s is a directory", cfg.Path)
	}
	mode := "rw"
	if cfg.ReadOnly {
		mode = "ro"
	}
	dsn := "file:" + cfg.Path + "?mode=" + mode + "&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection: PRAGMA state stays coherent and lock behavior is
	// predictable for an interactive tool.
	db.SetMaxOpenConns(1)
	c := &conn{db: db, path: cfg.Path}
	if err := c.Ping(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return c, nil
}

type conn struct {
	db   *sql.DB
	path string
}

func (c *conn) Ping(ctx context.Context) error {
	// PingContext alone does not touch the file for sqlite; force a read
	// so a non-database file fails here, not on first browse.
	var n int
	return c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master").Scan(&n)
}

func (c *conn) Close() error { return c.db.Close() }

func (c *conn) ServerInfo(ctx context.Context) ([]dbx.KV, error) {
	var out []dbx.KV
	var version string
	if err := c.db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version); err != nil {
		return nil, err
	}
	out = append(out, dbx.KV{Key: "engine", Value: "SQLite " + version})
	out = append(out, dbx.KV{Key: "file", Value: c.path})
	if fi, err := os.Stat(c.path); err == nil {
		out = append(out, dbx.KV{Key: "size", Value: fmt.Sprintf("%d bytes", fi.Size())})
	}
	for _, p := range []string{"journal_mode", "page_size", "encoding", "user_version"} {
		var v any
		if err := c.db.QueryRowContext(ctx, "PRAGMA "+p).Scan(&v); err == nil {
			out = append(out, dbx.KV{Key: p, Value: fmt.Sprint(v)})
		}
	}
	return out, nil
}

func (c *conn) Relations(ctx context.Context, _, _ string) ([]dbx.RelInfo, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT name, type FROM sqlite_master
		WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%'
		ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dbx.RelInfo
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			return nil, err
		}
		ri := dbx.RelInfo{Name: name, Kind: dbx.RelTable, RowsEst: -1, SizeBytes: -1}
		if typ == "view" {
			ri.Kind = dbx.RelView
		}
		out = append(out, ri)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Row counts (tables only), bounded so a huge table cannot stall the
	// listing. Views can run arbitrary SQL — never counted while browsing.
	for i := range out {
		if out[i].Kind != dbx.RelTable {
			continue
		}
		q := fmt.Sprintf("SELECT COUNT(*) FROM (SELECT 1 FROM %s LIMIT %d)",
			quoteIdent(out[i].Name), rowsEstCap+1)
		var n int64
		if err := c.db.QueryRowContext(ctx, q).Scan(&n); err == nil {
			out[i].RowsEst = n
		}
	}
	return out, nil
}

func (c *conn) Columns(ctx context.Context, ref dbx.ObjectRef) ([]dbx.ColInfo, error) {
	rows, err := c.db.QueryContext(ctx, "PRAGMA table_info("+quoteIdent(ref.Name)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dbx.ColInfo
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		out = append(out, dbx.ColInfo{
			Name:     name,
			TypeName: typ,
			NotNull:  notnull != 0,
			PK:       pk != 0,
			Default:  dflt.String,
		})
	}
	return out, rows.Err()
}

func (c *conn) Indexes(ctx context.Context, ref dbx.ObjectRef) ([]dbx.IndexInfo, error) {
	rows, err := c.db.QueryContext(ctx, "PRAGMA index_list("+quoteIdent(ref.Name)+")")
	if err != nil {
		return nil, err
	}
	var out []dbx.IndexInfo
	for rows.Next() {
		var seq, unique, partial int
		var name, origin string
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, dbx.IndexInfo{Name: name, Unique: unique != 0})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range out {
		cols, err := c.indexColumns(ctx, out[i].Name)
		if err == nil {
			out[i].Columns = cols
		}
	}
	return out, nil
}

func (c *conn) indexColumns(ctx context.Context, index string) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, "PRAGMA index_info("+quoteIdent(index)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var seqno, cid int
		var name sql.NullString // NULL for expression index columns
		if err := rows.Scan(&seqno, &cid, &name); err != nil {
			return nil, err
		}
		if name.Valid {
			cols = append(cols, name.String)
		} else {
			cols = append(cols, "<expr>")
		}
	}
	return cols, rows.Err()
}

// quoteIdent double-quotes an identifier, escaping embedded quotes.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
