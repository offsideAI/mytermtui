package postgres

import (
	"context"

	"github.com/offsideai/mydb/internal/dbx"
)

func (c *conn) Databases(ctx context.Context) ([]dbx.DBInfo, error) {
	p, err := c.poolFor(ctx, "")
	if err != nil {
		return nil, err
	}
	// Sizes need CONNECT privilege; fall back to names-only rather than
	// hiding databases from a low-privilege user.
	rows, err := p.Query(ctx, `
		SELECT d.datname, pg_get_userbyid(d.datdba),
		       CASE WHEN has_database_privilege(d.datname, 'CONNECT')
		            THEN pg_database_size(d.datname) ELSE -1 END
		FROM pg_database d
		WHERE d.datallowconn AND NOT d.datistemplate
		ORDER BY d.datname`)
	if err != nil {
		rows, err = p.Query(ctx, `
			SELECT d.datname, pg_get_userbyid(d.datdba), -1::bigint
			FROM pg_database d
			WHERE d.datallowconn AND NOT d.datistemplate
			ORDER BY d.datname`)
		if err != nil {
			return nil, err
		}
	}
	defer rows.Close()
	var out []dbx.DBInfo
	for rows.Next() {
		var d dbx.DBInfo
		if err := rows.Scan(&d.Name, &d.Owner, &d.SizeBytes); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (c *conn) Schemas(ctx context.Context, db string) ([]dbx.SchemaInfo, error) {
	p, err := c.poolFor(ctx, db)
	if err != nil {
		return nil, err
	}
	rows, err := p.Query(ctx, `
		SELECT n.nspname, pg_get_userbyid(n.nspowner)
		FROM pg_namespace n
		WHERE n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'
		ORDER BY n.nspname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dbx.SchemaInfo
	for rows.Next() {
		var s dbx.SchemaInfo
		if err := rows.Scan(&s.Name, &s.Owner); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (c *conn) Relations(ctx context.Context, db, schema string) ([]dbx.RelInfo, error) {
	p, err := c.poolFor(ctx, db)
	if err != nil {
		return nil, err
	}
	// r = table, p = partitioned table, v = view, m = materialized view.
	// reltuples is the planner's free estimate: -1 = never analyzed.
	rows, err := p.Query(ctx, `
		SELECT c.relname, c.relkind::text, c.reltuples::bigint,
		       pg_total_relation_size(c.oid)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind IN ('r','p','v','m')
		ORDER BY c.relname`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dbx.RelInfo
	for rows.Next() {
		var name, kind string
		var ri dbx.RelInfo
		if err := rows.Scan(&name, &kind, &ri.RowsEst, &ri.SizeBytes); err != nil {
			return nil, err
		}
		ri.Name = name
		if kind == "v" || kind == "m" {
			ri.Kind = dbx.RelView
			ri.SizeBytes = -1
			if kind == "v" {
				ri.RowsEst = -1
			}
		}
		out = append(out, ri)
	}
	return out, rows.Err()
}

func (c *conn) Columns(ctx context.Context, ref dbx.ObjectRef) ([]dbx.ColInfo, error) {
	p, err := c.poolFor(ctx, ref.Database)
	if err != nil {
		return nil, err
	}
	rows, err := p.Query(ctx, `
		SELECT a.attname,
		       format_type(a.atttypid, a.atttypmod),
		       a.attnotnull,
		       COALESCE(pg_get_expr(ad.adbin, ad.adrelid), ''),
		       EXISTS (SELECT 1 FROM pg_index i
		               WHERE i.indrelid = a.attrelid AND i.indisprimary
		                 AND a.attnum = ANY (i.indkey))
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
		WHERE n.nspname = $1 AND c.relname = $2
		  AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum`, ref.Schema, ref.Name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dbx.ColInfo
	for rows.Next() {
		var ci dbx.ColInfo
		if err := rows.Scan(&ci.Name, &ci.TypeName, &ci.NotNull, &ci.Default, &ci.PK); err != nil {
			return nil, err
		}
		out = append(out, ci)
	}
	return out, rows.Err()
}

func (c *conn) Indexes(ctx context.Context, ref dbx.ObjectRef) ([]dbx.IndexInfo, error) {
	p, err := c.poolFor(ctx, ref.Database)
	if err != nil {
		return nil, err
	}
	rows, err := p.Query(ctx, `
		SELECT i.relname, x.indisunique,
		       ARRAY(SELECT pg_get_indexdef(x.indexrelid, k.n, true)
		             FROM generate_subscripts(x.indkey, 1) AS k(n)
		             ORDER BY k.n)
		FROM pg_index x
		JOIN pg_class c ON c.oid = x.indrelid
		JOIN pg_class i ON i.oid = x.indexrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2
		ORDER BY i.relname`, ref.Schema, ref.Name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dbx.IndexInfo
	for rows.Next() {
		var ix dbx.IndexInfo
		if err := rows.Scan(&ix.Name, &ix.Unique, &ix.Columns); err != nil {
			return nil, err
		}
		out = append(out, ix)
	}
	return out, rows.Err()
}

func (c *conn) Roles(ctx context.Context) ([]dbx.RoleInfo, error) {
	p, err := c.poolFor(ctx, "")
	if err != nil {
		return nil, err
	}
	rows, err := p.Query(ctx, `
		SELECT r.rolname, r.rolcanlogin, r.rolsuper,
		       ARRAY(SELECT b.rolname FROM pg_auth_members mm
		             JOIN pg_roles b ON b.oid = mm.roleid
		             WHERE mm.member = r.oid ORDER BY b.rolname)
		FROM pg_roles r
		WHERE r.rolname NOT LIKE 'pg\_%'
		ORDER BY r.rolname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dbx.RoleInfo
	for rows.Next() {
		var r dbx.RoleInfo
		if err := rows.Scan(&r.Name, &r.CanLogin, &r.Super, &r.MemberOf); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
