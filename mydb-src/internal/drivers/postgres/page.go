package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/offsideai/mydb/internal/dbx"
)

// ReadPage returns one stable-ordered page: primary key when one exists,
// ctid for other tables (physical but stable enough between pages of an
// idle admin session), first column for views.
func (c *conn) ReadPage(ctx context.Context, ref dbx.ObjectRef, pr dbx.PageReq) (*dbx.Result, error) {
	if pr.Limit <= 0 {
		pr.Limit = 500
	}
	kind, err := c.relkind(ctx, ref)
	if err != nil {
		return nil, err
	}
	order, err := c.stableOrder(ctx, ref, kind)
	if err != nil {
		return nil, err
	}
	p, err := c.poolFor(ctx, ref.Database)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	q := fmt.Sprintf("SELECT * FROM %s%s LIMIT %d OFFSET %d",
		qualified(ref), order, pr.Limit+1, pr.Offset)
	rows, err := p.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fds := rows.FieldDescriptions()
	res := &dbx.Result{Columns: make([]dbx.Column, len(fds))}
	conn := rows.Conn()
	for i, fd := range fds {
		res.Columns[i] = dbx.Column{Name: fd.Name}
		if conn != nil {
			if t, ok := conn.TypeMap().TypeForOID(fd.DataTypeOID); ok {
				res.Columns[i].TypeName = t.Name
			}
		}
	}
	for rows.Next() {
		if len(res.Rows) == pr.Limit {
			res.Truncated = true
			break
		}
		vals, err := rows.Values()
		if err != nil {
			return nil, err
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

func (c *conn) stableOrder(ctx context.Context, ref dbx.ObjectRef, kind string) (string, error) {
	p, err := c.poolFor(ctx, ref.Database)
	if err != nil {
		return "", err
	}
	rows, err := p.Query(ctx, `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY (i.indkey)
		WHERE n.nspname = $1 AND c.relname = $2 AND i.indisprimary
		ORDER BY array_position(i.indkey, a.attnum)`, ref.Schema, ref.Name)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var pk []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return "", err
		}
		pk = append(pk, quoteIdent(col))
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(pk) > 0 {
		return " ORDER BY " + strings.Join(pk, ", "), nil
	}
	if kind == "r" || kind == "p" || kind == "m" {
		return " ORDER BY ctid", nil
	}
	return " ORDER BY 1", nil
}
