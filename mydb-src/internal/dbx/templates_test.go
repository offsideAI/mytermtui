package dbx

import (
	"strings"
	"testing"
)

func TestQuoting(t *testing.T) {
	if got := QuoteIdent(`we"ird`); got != `"we""ird"` {
		t.Errorf("QuoteIdent = %s", got)
	}
	if got := QuoteLiteral("it's"); got != "'it''s'" {
		t.Errorf("QuoteLiteral = %s", got)
	}
	if ValidIdent("") || ValidIdent("a\x00b") || !ValidIdent("ok name") {
		t.Error("ValidIdent rules wrong")
	}
}

func TestTemplateBuilds(t *testing.T) {
	byKey := map[string]Template{}
	for _, tpl := range Catalog {
		byKey[tpl.Key] = tpl
	}

	p := byKey["create-user"].Build(map[string]string{"user": "alice", "password": "p'w"})
	if p.Stmts[0] != `CREATE USER "alice" WITH PASSWORD 'p''w'` {
		t.Errorf("create-user: %s", p.Stmts[0])
	}
	if p.Danger != DangerMedium {
		t.Error("create-user should be Medium")
	}

	p = byKey["create-db"].Build(map[string]string{"db": "appdb", "owner": "alice"})
	if p.Stmts[0] != `CREATE DATABASE "appdb" OWNER "alice"` {
		t.Errorf("create-db: %s", p.Stmts[0])
	}

	p = byKey["grant-all"].Build(map[string]string{"db": "appdb", "user": "alice"})
	if p.Stmts[0] != `GRANT ALL PRIVILEGES ON DATABASE "appdb" TO "alice"` {
		t.Errorf("grant-all: %s", p.Stmts[0])
	}

	p = byKey["drop-db"].Build(map[string]string{"db": "appdb"})
	if p.Danger != DangerHigh || p.ConfirmToken != "appdb" {
		t.Errorf("drop-db must be High with the db name as token: %+v", p)
	}

	p = byKey["grant-readonly"].Build(map[string]string{"schema": "public", "user": "bob"})
	if len(p.Stmts) != 3 || !p.Tx {
		t.Errorf("grant-readonly should be a 3-stmt transactional plan: %+v", p)
	}
}

func TestPlanScriptTxWrap(t *testing.T) {
	p := Plan{Stmts: []string{"A", "B"}, Tx: true}
	if got := p.Script(true); got != "BEGIN;\nA;\nB;\nCOMMIT;" {
		t.Errorf("explicit tx: %q", got)
	}
	if got := p.Script(false); got != "A;\nB;" {
		t.Errorf("implicit tx: %q", got)
	}
	single := Plan{Stmts: []string{"CREATE DATABASE x"}, Tx: true}
	if got := single.Script(true); strings.Contains(got, "BEGIN") {
		t.Errorf("single statements must never be wrapped: %q", got)
	}
}

func TestMaintenanceFor(t *testing.T) {
	ops := MaintenanceFor("sqlite", ObjectRef{})
	if len(ops) != 4 || ops[0].Label != "VACUUM" {
		t.Fatalf("sqlite db-level ops: %+v", ops)
	}
	if ops[3].Plan.Danger != DangerNone {
		t.Error("integrity check is a pure read")
	}
	ops = MaintenanceFor("postgres", ObjectRef{Database: "app", Schema: "public", Name: "users"})
	if len(ops) != 3 || !strings.Contains(ops[0].Plan.Stmts[0], `"public"."users"`) {
		t.Fatalf("pg table ops: %+v", ops)
	}
	if ops[0].Plan.DB != "app" {
		t.Error("table ops must target the table's database")
	}
}
