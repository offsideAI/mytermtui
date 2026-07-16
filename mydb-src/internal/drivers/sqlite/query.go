package sqlite

import (
	"context"
	"time"

	"github.com/offsideai/mydb/internal/dbx"
)

// Query runs a user script statement by statement (SQLite's driver
// executes one statement per call). The result carries the LAST result
// set plus statement count and total affected rows. Context cancellation
// interrupts the running statement.
func (c *conn) Query(ctx context.Context, _ string, script string, req dbx.QueryReq) (*dbx.Result, error) {
	if req.MaxRows <= 0 {
		req.MaxRows = 10000
	}
	stmts := dbx.SplitStatements(script)
	res := &dbx.Result{}
	start := time.Now()
	for _, stmt := range stmts {
		if dbx.ReturnsRows(stmt) {
			set, err := c.queryOne(ctx, stmt, req.MaxRows)
			if err != nil {
				return nil, err
			}
			set.Stmts = res.Stmts + 1
			set.Affected = res.Affected
			res = set
		} else {
			out, err := c.db.ExecContext(ctx, stmt)
			if err != nil {
				return nil, err
			}
			if n, err := out.RowsAffected(); err == nil {
				res.Affected += n
			}
			res.Stmts++
		}
	}
	res.Elapsed = time.Since(start)
	return res, nil
}

func (c *conn) queryOne(ctx context.Context, stmt string, maxRows int) (*dbx.Result, error) {
	rows, err := c.db.QueryContext(ctx, stmt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	names, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	res := &dbx.Result{Columns: make([]dbx.Column, len(names))}
	for i, n := range names {
		res.Columns[i] = dbx.Column{Name: n}
	}
	if cts, err := rows.ColumnTypes(); err == nil {
		for i, ct := range cts {
			if i < len(res.Columns) {
				res.Columns[i].TypeName = ct.DatabaseTypeName()
			}
		}
	}
	vals := make([]any, len(names))
	ptrs := make([]any, len(names))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if len(res.Rows) == maxRows {
			res.Truncated = true
			break
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]dbx.Value, len(vals))
		for i, v := range vals {
			row[i] = dbx.Render(v)
		}
		res.Rows = append(res.Rows, row)
	}
	return res, rows.Err()
}
