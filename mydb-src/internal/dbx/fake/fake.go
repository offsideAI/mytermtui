// Package fake is an in-memory multi-database dbx.Driver for tests and
// the screenshot harness: deterministic data, no I/O, instant answers.
package fake

import (
	"context"
	"fmt"

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

func (Driver) Open(_ context.Context, cfg dbx.ConnConfig) (dbx.Conn, error) {
	return &Conn{}, nil
}

type Conn struct {
	// LastSQL records the most recent Query script — tests assert on it.
	LastSQL string
}

func (*Conn) Ping(context.Context) error { return nil }
func (*Conn) Close() error               { return nil }

func (*Conn) ServerInfo(context.Context) ([]dbx.KV, error) {
	return []dbx.KV{
		{Key: "engine", Value: "FakePG 1.0 (in-memory)"},
		{Key: "database", Value: "appdb"},
		{Key: "encoding", Value: "UTF8"},
	}, nil
}

func (*Conn) Databases(context.Context) ([]dbx.DBInfo, error) {
	return []dbx.DBInfo{
		{Name: "appdb", Owner: "u", SizeBytes: 100},
		{Name: "postgres", Owner: "u", SizeBytes: 200},
		{Name: "sibling_a", Owner: "u", SizeBytes: 300},
		{Name: "sibling_b", Owner: "u", SizeBytes: 400},
	}, nil
}

func (*Conn) Schemas(_ context.Context, db string) ([]dbx.SchemaInfo, error) {
	return []dbx.SchemaInfo{{Name: "public", Owner: "owner-" + db}}, nil
}

func (*Conn) Relations(_ context.Context, db, _ string) ([]dbx.RelInfo, error) {
	return []dbx.RelInfo{{Name: db + "_t", Kind: dbx.RelTable, RowsEst: 1, SizeBytes: 10}}, nil
}

func (*Conn) Columns(context.Context, dbx.ObjectRef) ([]dbx.ColInfo, error) {
	return []dbx.ColInfo{{Name: "id", TypeName: "int", PK: true}}, nil
}

func (*Conn) Indexes(context.Context, dbx.ObjectRef) ([]dbx.IndexInfo, error) {
	return nil, nil
}

func (*Conn) Roles(context.Context) ([]dbx.RoleInfo, error) {
	return []dbx.RoleInfo{
		{Name: "admin", CanLogin: true, MemberOf: []string{"pg_monitor"}},
	}, nil
}

func (*Conn) ReadPage(_ context.Context, ref dbx.ObjectRef, pr dbx.PageReq) (*dbx.Result, error) {
	res := &dbx.Result{Columns: []dbx.Column{
		{Name: "id", TypeName: "int"}, {Name: "label", TypeName: "text"},
	}}
	for i := 0; i < pr.Limit && i < 5; i++ {
		n := pr.Offset + i + 1
		res.Rows = append(res.Rows, []dbx.Value{
			{Kind: dbx.VNumber, S: fmt.Sprint(n)},
			{Kind: dbx.VText, S: fmt.Sprintf("%s row %d", ref.Name, n)},
		})
	}
	return res, nil
}

func (c *Conn) Query(_ context.Context, _, sql string, _ dbx.QueryReq) (*dbx.Result, error) {
	c.LastSQL = sql
	return &dbx.Result{
		Columns: []dbx.Column{{Name: "echo", TypeName: "text"}},
		Rows:    [][]dbx.Value{{{Kind: dbx.VText, S: sql}}},
		Stmts:   1,
	}, nil
}
