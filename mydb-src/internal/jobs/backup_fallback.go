package jobs

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/offsideai/mydb/internal/dbx"
)

// PlainSQLDump is the built-in fallback for when pg_dump is unavailable
// (or prefer_pg_dump=false): it writes the database's data as INSERT
// statements by querying every table through the live connection. It is
// deliberately limited — no schema (CREATE TABLE), no sequences, no
// large objects, no privileges — and the artifact opens with a warning
// saying so (spec §1.6). Faithful backups need pg_dump.
func PlainSQLDump(conn dbx.Conn, database string, schemas []string, dest string) Run {
	return func(ctx context.Context, report Report) error {
		if _, err := os.Stat(dest); err == nil {
			return fmt.Errorf("%s already exists", dest)
		}
		f, err := os.Create(dest)
		if err != nil {
			return err
		}
		w := bufio.NewWriter(f)
		defer func() { w.Flush(); f.Close() }()

		fmt.Fprintf(w, "-- mydb plain-SQL dump of database %q\n", database)
		fmt.Fprintln(w, "-- WARNING: data only. No schema, sequences, large objects,")
		fmt.Fprintln(w, "-- or privileges. Not a faithful backup — use pg_dump for that.")
		fmt.Fprintln(w)

		for _, schema := range schemas {
			rels, err := conn.Relations(ctx, database, schema)
			if err != nil {
				return err
			}
			for _, rel := range rels {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if rel.Kind != dbx.RelTable {
					continue
				}
				report(int64(w.Buffered()), -1, "dumping "+schema+"."+rel.Name)
				if err := dumpTable(ctx, conn, w, dbx.ObjectRef{Database: database, Schema: schema, Name: rel.Name}); err != nil {
					return err
				}
			}
		}
		report(-1, -1, "done")
		return nil
	}
}

// dumpTable writes one table's rows as INSERT statements.
func dumpTable(ctx context.Context, conn dbx.Conn, w *bufio.Writer, ref dbx.ObjectRef) error {
	cols, err := conn.Columns(ctx, ref)
	if err != nil {
		return err
	}
	colNames := make([]string, len(cols))
	for i, c := range cols {
		colNames[i] = dbx.QuoteIdent(c.Name)
	}
	target := dbx.QuoteIdent(ref.Schema) + "." + dbx.QuoteIdent(ref.Name)

	// One SELECT, capped high — a fallback dump of an enormous table is
	// already the wrong tool, but we bound memory rather than stream.
	res, err := conn.Query(ctx, ref.Database,
		fmt.Sprintf("SELECT %s FROM %s", strings.Join(colNames, ", "), target),
		dbx.QueryReq{MaxRows: 1_000_000})
	if err != nil {
		return err
	}
	prefix := fmt.Sprintf("INSERT INTO %s (%s) VALUES ", target, strings.Join(colNames, ", "))
	for _, row := range res.Rows {
		vals := make([]string, len(row))
		for i, v := range row {
			vals[i] = sqlValue(v)
		}
		fmt.Fprintf(w, "%s(%s);\n", prefix, strings.Join(vals, ", "))
	}
	if res.Truncated {
		fmt.Fprintf(w, "-- WARNING: %s truncated at the row cap; dump is incomplete\n", target)
	}
	fmt.Fprintln(w)
	return nil
}

// sqlValue renders a cell as a SQL literal.
func sqlValue(v dbx.Value) string {
	switch v.Kind {
	case dbx.VNull:
		return "NULL"
	case dbx.VNumber, dbx.VBool:
		return v.S
	case dbx.VBytes:
		// Rendered as 0x… by dbx.Render; emit a bytea hex literal.
		if strings.HasPrefix(v.S, "0x") {
			return "'\\x" + v.S[2:] + "'"
		}
		return dbx.QuoteLiteral(v.S)
	default:
		return dbx.QuoteLiteral(v.S)
	}
}
