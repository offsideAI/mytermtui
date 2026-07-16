// Package postgres implements dbx.Driver over jackc/pgx/v5, used
// natively (not through database/sql) so server-side query cancellation,
// rich error positions, and multi-statement scripts stay available.
// Introspection reads pg_catalog.
package postgres

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/offsideai/mydb/internal/dbx"
)

type Driver struct{}

func New() Driver { return Driver{} }

func (Driver) Engine() string { return "postgres" }

func (Driver) Capabilities() dbx.Capabilities {
	return dbx.Capabilities{
		MultipleDatabases: true,
		Schemas:           true,
		Roles:             true,
		TransactionalDDL:  true,
		ServerCancel:      true,
		Explain:           "EXPLAIN ",
		ExplainAnalyze:    "EXPLAIN (ANALYZE, BUFFERS) ",
	}
}

func (Driver) Open(ctx context.Context, cfg dbx.ConnConfig) (dbx.Conn, error) {
	c := &conn{cfg: cfg, pools: map[string]*pgxpool.Pool{}}
	if _, err := c.poolFor(ctx, ""); err != nil {
		return nil, err
	}
	return c, nil
}

// conn holds one pgx pool per browsed database: a Postgres connection is
// bound to a single database, so expanding another database on the same
// server opens a sibling pool under the hood (spec §1.4).
type conn struct {
	cfg dbx.ConnConfig

	mu    sync.Mutex
	pools map[string]*pgxpool.Pool // dbname → pool; "" = the configured db
}

// poolFor returns (creating on demand) the pool for a database.
func (c *conn) poolFor(ctx context.Context, db string) (*pgxpool.Pool, error) {
	if db == c.cfg.DBName {
		db = "" // the configured database reuses the primary pool
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.pools[db]; ok {
		return p, nil
	}
	pc, err := pgxpool.ParseConfig("")
	if err != nil {
		return nil, err
	}
	pc.ConnConfig.Host = c.cfg.Host
	if c.cfg.Port > 0 {
		pc.ConnConfig.Port = uint16(c.cfg.Port)
	}
	pc.ConnConfig.User = c.cfg.Username
	if c.cfg.Password != "" {
		pc.ConnConfig.Password = c.cfg.Password
	}
	dbname := db
	if dbname == "" {
		dbname = c.cfg.DBName
	}
	pc.ConnConfig.Database = dbname
	// An interactive browser needs few connections; introspection bursts
	// share them.
	pc.MaxConns = 3
	if c.cfg.ReadOnly {
		pc.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	}
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	c.pools[db] = pool
	return pool, nil
}

func (c *conn) Ping(ctx context.Context) error {
	p, err := c.poolFor(ctx, "")
	if err != nil {
		return err
	}
	return p.Ping(ctx)
}

func (c *conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range c.pools {
		p.Close()
	}
	c.pools = map[string]*pgxpool.Pool{}
	return nil
}

func (c *conn) ServerInfo(ctx context.Context) ([]dbx.KV, error) {
	p, err := c.poolFor(ctx, "")
	if err != nil {
		return nil, err
	}
	var out []dbx.KV
	var version string
	if err := p.QueryRow(ctx, "SELECT version()").Scan(&version); err != nil {
		return nil, err
	}
	out = append(out, dbx.KV{Key: "engine", Value: version})
	for _, q := range []struct{ key, sql string }{
		{"database", "SELECT current_database()"},
		{"user", "SELECT current_user"},
		{"encoding", "SHOW server_encoding"},
		{"size", "SELECT pg_size_pretty(pg_database_size(current_database()))"},
	} {
		var v string
		if err := p.QueryRow(ctx, q.sql).Scan(&v); err == nil {
			out = append(out, dbx.KV{Key: q.key, Value: v})
		}
	}
	return out, nil
}

// quoteIdent double-quotes an identifier, escaping embedded quotes.
func quoteIdent(s string) string {
	out := make([]rune, 0, len(s)+2)
	out = append(out, '"')
	for _, r := range s {
		if r == '"' {
			out = append(out, '"')
		}
		out = append(out, r)
	}
	return string(append(out, '"'))
}

func qualified(ref dbx.ObjectRef) string {
	if ref.Schema == "" {
		return quoteIdent(ref.Name)
	}
	return quoteIdent(ref.Schema) + "." + quoteIdent(ref.Name)
}

// relkind fetches the pg_class relkind for a relation, erroring for
// unknown names so ReadPage can never run over a mistyped identifier.
func (c *conn) relkind(ctx context.Context, ref dbx.ObjectRef) (string, error) {
	p, err := c.poolFor(ctx, ref.Database)
	if err != nil {
		return "", err
	}
	var kind string
	err = p.QueryRow(ctx, `
		SELECT c.relkind::text FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2`, ref.Schema, ref.Name).Scan(&kind)
	if err == pgx.ErrNoRows {
		return "", fmt.Errorf("unknown relation %s", qualified(ref))
	}
	return kind, err
}
