package dbx

import "strings"

// Danger classifies a UI-generated operation. It is assigned statically
// by the action that builds the plan — never by parsing SQL (§4.6).
type Danger int

const (
	DangerNone   Danger = iota // reads; run without ceremony
	DangerMedium               // CREATE/ALTER/GRANT/maintenance: y/n confirm
	DangerHigh                 // DROP/TRUNCATE/role deletion: typed confirm
)

// Plan is the single choke point every UI-generated write flows
// through: the exact statements, their danger tier, and what the
// confirmation dialog must show. Raw SQL typed in the editor never
// becomes a Plan — the editor is the expert path.
type Plan struct {
	Title        string // "Create user alice"
	Stmts        []string
	Danger       Danger
	Summary      string // one-line blast radius for the dialog
	ConfirmToken string // DangerHigh: the exact text the user must type
	Tx           bool   // wrap multi-statement plans in a transaction
	DB           string // target database ("" = the connection's own)
}

// Script renders the statements for execution. explicitTx wraps in
// BEGIN/COMMIT — used for engines that execute statement-by-statement
// (SQLite); Postgres' simple protocol already runs a multi-statement
// script in one implicit transaction (and single statements like
// CREATE DATABASE must NOT be wrapped).
func (p Plan) Script(explicitTx bool) string {
	body := strings.Join(p.Stmts, ";\n") + ";"
	if explicitTx && p.Tx && len(p.Stmts) > 1 {
		return "BEGIN;\n" + body + "\nCOMMIT;"
	}
	return body
}
