package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/offsideai/mydb/internal/dbx"
)

// Query runs a user script through the simple protocol, which executes
// multi-statement scripts natively and returns one result per statement.
// Values arrive in text format — exactly what a display grid wants.
// Context cancellation sends a server-side cancel request.
func (c *conn) Query(ctx context.Context, db, script string, req dbx.QueryReq) (*dbx.Result, error) {
	if req.MaxRows <= 0 {
		req.MaxRows = 10000
	}
	p, err := c.poolFor(ctx, db)
	if err != nil {
		return nil, err
	}
	pc, err := p.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer pc.Release()

	res := &dbx.Result{}
	start := time.Now()
	mrr := pc.Conn().PgConn().Exec(ctx, script)
	for mrr.NextResult() {
		rr := mrr.ResultReader()
		fds := rr.FieldDescriptions()
		if len(fds) > 0 {
			set := &dbx.Result{Columns: make([]dbx.Column, len(fds))}
			for i, fd := range fds {
				set.Columns[i] = dbx.Column{Name: fd.Name, TypeName: typeNameForOID(fd.DataTypeOID)}
			}
			for rr.NextRow() {
				if len(set.Rows) == req.MaxRows {
					set.Truncated = true
					continue // keep draining the protocol
				}
				raw := rr.Values()
				row := make([]dbx.Value, len(raw))
				for i, b := range raw {
					row[i] = textValue(fds[i].DataTypeOID, b)
				}
				set.Rows = append(set.Rows, row)
			}
			if _, err := rr.Close(); err != nil {
				mrr.Close()
				return nil, renderErr(err, script)
			}
			// A SELECT's command tag "counts" its rows — that is not DML
			// "affected", so only Exec-style statements accumulate.
			set.Stmts = res.Stmts + 1
			set.Affected = res.Affected
			res = set
		} else {
			ct, err := rr.Close()
			if err != nil {
				mrr.Close()
				return nil, renderErr(err, script)
			}
			res.Affected += ct.RowsAffected()
			res.Stmts++
		}
	}
	if err := mrr.Close(); err != nil {
		return nil, renderErr(err, script)
	}
	res.Elapsed = time.Since(start)
	return res, nil
}

// textValue tags a text-format wire value with a display kind.
func textValue(oid uint32, b []byte) dbx.Value {
	if b == nil {
		return dbx.Value{Kind: dbx.VNull}
	}
	s := string(b)
	if len(s) > 4096 {
		s = s[:4096] + fmt.Sprintf("… (%d bytes)", len(b))
	}
	switch oid {
	case 16: // bool
		return dbx.Value{Kind: dbx.VBool, S: s}
	case 20, 21, 23, 26, 700, 701, 1700: // ints, oid, floats, numeric
		return dbx.Value{Kind: dbx.VNumber, S: s}
	case 17: // bytea (\x…)
		return dbx.Value{Kind: dbx.VBytes, S: s}
	case 1082, 1083, 1114, 1184: // date, time, timestamp[tz]
		return dbx.Value{Kind: dbx.VTime, S: s}
	}
	return dbx.Value{Kind: dbx.VText, S: s}
}

func typeNameForOID(oid uint32) string {
	switch oid {
	case 16:
		return "bool"
	case 17:
		return "bytea"
	case 20:
		return "int8"
	case 21:
		return "int2"
	case 23:
		return "int4"
	case 25:
		return "text"
	case 700:
		return "float4"
	case 701:
		return "float8"
	case 1042:
		return "bpchar"
	case 1043:
		return "varchar"
	case 1082:
		return "date"
	case 1114:
		return "timestamp"
	case 1184:
		return "timestamptz"
	case 1700:
		return "numeric"
	case 2950:
		return "uuid"
	case 3802:
		return "jsonb"
	}
	return ""
}

// renderErr enriches Postgres errors with position (as line:col into the
// script) and hint, so the SQL tab can show exactly where things broke.
func renderErr(err error, script string) error {
	var pge *pgconn.PgError
	if !errors.As(err, &pge) {
		return err
	}
	msg := pge.Message
	if pge.Position > 0 {
		line, col := 1, 1
		for i, r := range script {
			if i >= int(pge.Position)-1 {
				break
			}
			if r == '\n' {
				line++
				col = 1
			} else {
				col++
			}
		}
		msg += fmt.Sprintf(" (line %d, col %d)", line, col)
	}
	if pge.Detail != "" {
		msg += " — " + pge.Detail
	}
	if pge.Hint != "" {
		msg += " — hint: " + pge.Hint
	}
	return errors.New(strings.TrimSpace(msg))
}
