package sqlite

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/offsideai/mydb/internal/dbx"
)

// Introspector methods behind capability flags SQLite doesn't have —
// never called by the UI; present to satisfy dbx.Conn.

var errUnsupported = errors.New("sqlite: not supported")

func (c *conn) Databases(context.Context) ([]dbx.DBInfo, error) { return nil, errUnsupported }

func (c *conn) Schemas(context.Context, string) ([]dbx.SchemaInfo, error) {
	return nil, errUnsupported
}

func (c *conn) Roles(context.Context) ([]dbx.RoleInfo, error) { return nil, errUnsupported }

// ReadPage returns one stable-ordered page of a table or view.
func (c *conn) ReadPage(ctx context.Context, ref dbx.ObjectRef, pr dbx.PageReq) (*dbx.Result, error) {
	if pr.Limit <= 0 {
		pr.Limit = 500
	}
	order, err := c.stableOrder(ctx, ref.Name)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	// One row beyond the page signals Truncated without a COUNT(*).
	q := fmt.Sprintf("SELECT * FROM %s%s LIMIT %d OFFSET %d",
		quoteIdent(ref.Name), order, pr.Limit+1, pr.Offset)
	rows, err := c.db.QueryContext(ctx, q)
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
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		if len(res.Rows) == pr.Limit {
			res.Truncated = true
			break
		}
		row := make([]dbx.Value, len(vals))
		for i, v := range vals {
			row[i] = dbx.Render(v)
		}
		res.Rows = append(res.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	res.Elapsed = time.Since(start)
	return res, nil
}

// stableOrder picks a deterministic ORDER BY: primary key when declared
// (WITHOUT ROWID tables always have one), rowid for other tables, and
// the first column for views (which have no physical order at all).
func (c *conn) stableOrder(ctx context.Context, name string) (string, error) {
	var typ string
	err := c.db.QueryRowContext(ctx,
		`SELECT type FROM sqlite_master WHERE name = ?`, name).Scan(&typ)
	if err != nil {
		return "", fmt.Errorf("unknown relation %q: %w", name, err)
	}

	rows, err := c.db.QueryContext(ctx, "PRAGMA table_info("+quoteIdent(name)+")")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	// pk position (1-based) → column name
	pk := map[int]string{}
	for rows.Next() {
		var cid, notnull, pkPos int
		var colName, colType string
		var dflt any
		if err := rows.Scan(&cid, &colName, &colType, &notnull, &dflt, &pkPos); err != nil {
			return "", err
		}
		if pkPos > 0 {
			pk[pkPos] = colName
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(pk) > 0 {
		cols := make([]string, 0, len(pk))
		for i := 1; i <= len(pk); i++ {
			cols = append(cols, quoteIdent(pk[i]))
		}
		return " ORDER BY " + strings.Join(cols, ", "), nil
	}
	if typ == "table" {
		return " ORDER BY rowid", nil
	}
	return " ORDER BY 1", nil
}
