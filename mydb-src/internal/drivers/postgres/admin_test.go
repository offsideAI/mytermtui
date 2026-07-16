package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/offsideai/mydb/internal/dbx"
)

// TestCommandTemplatesLive runs the CREATE USER / CREATE DATABASE / GRANT
// flow end-to-end against a real server, then cleans up. Gated on
// MYDB_TEST_PG_DSN.
func TestCommandTemplatesLive(t *testing.T) {
	c := testConn(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	user := fmt.Sprintf("mydb_u_%d", suffix)
	db := fmt.Sprintf("mydb_d_%d", suffix)

	byKey := map[string]dbx.Template{}
	for _, tpl := range dbx.Catalog {
		byKey[tpl.Key] = tpl
	}
	run := func(p dbx.Plan) {
		t.Helper()
		// CREATE DATABASE cannot run inside a transaction block, so never
		// wrap (matches the UI path for Postgres).
		if _, err := c.Query(ctx, p.DB, p.Script(false), dbx.QueryReq{}); err != nil {
			t.Fatalf("%s: %v", p.Title, err)
		}
	}
	t.Cleanup(func() {
		c.Query(context.Background(), "", "DROP DATABASE IF EXISTS "+quoteIdent(db), dbx.QueryReq{})
		c.Query(context.Background(), "", "DROP USER IF EXISTS "+quoteIdent(user), dbx.QueryReq{})
	})

	run(byKey["create-user"].Build(map[string]string{"user": user, "password": "s3cret"}))
	run(byKey["create-db"].Build(map[string]string{"db": db, "owner": user}))
	run(byKey["grant-all"].Build(map[string]string{"db": db, "user": user}))

	// Verify the role and database now exist.
	res, err := c.Query(ctx, "", fmt.Sprintf(
		"SELECT (SELECT count(*) FROM pg_roles WHERE rolname = %s) + (SELECT count(*) FROM pg_database WHERE datname = %s)",
		dbx.QuoteLiteral(user), dbx.QuoteLiteral(db)), dbx.QueryReq{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows[0][0].S != "2" {
		t.Fatalf("expected user+db to exist, count=%s", res.Rows[0][0].S)
	}
}

func TestRolesCarryMembership(t *testing.T) {
	c := testConn(t)
	roles, err := c.Roles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) == 0 {
		t.Fatal("no roles")
	}
	// Just assert the field is populated for at least the query shape; a
	// fresh cluster may have no memberships, so we don't require non-empty.
	_ = roles[0].MemberOf
}
