package dbx

import "fmt"

// The common-commands catalog (§3.5): the repetitive incantations as
// data. Adding a template is adding an entry — parameters are collected
// by dialogs, identifiers quoted via QuoteIdent, secrets via
// QuoteLiteral, and the rendered SQL is previewed before the
// danger-appropriate confirmation.

type ParamKind int

const (
	ParamIdent  ParamKind = iota // quoted as an identifier
	ParamSecret                  // masked input, quoted as a literal
)

type Param struct {
	Name  string // key into the values map
	Label string // dialog prompt
	Kind  ParamKind
}

type Template struct {
	Key    string
	Label  string
	Engine string // "postgres" | "sqlite" | "" = any
	Danger Danger
	Params []Param
	// Build renders the plan from collected values (already validated:
	// idents pass ValidIdent, none empty).
	Build func(v map[string]string) Plan
}

// Catalog is ordered as shown in the Commands menu.
var Catalog = []Template{
	{
		Key: "create-user", Label: "Create user…", Engine: "postgres", Danger: DangerMedium,
		Params: []Param{
			{Name: "user", Label: "User name", Kind: ParamIdent},
			{Name: "password", Label: "Password", Kind: ParamSecret},
		},
		Build: func(v map[string]string) Plan {
			return Plan{
				Title:   "Create user " + v["user"],
				Stmts:   []string{fmt.Sprintf("CREATE USER %s WITH PASSWORD %s", QuoteIdent(v["user"]), QuoteLiteral(v["password"]))},
				Danger:  DangerMedium,
				Summary: "creates login role " + v["user"],
			}
		},
	},
	{
		Key: "create-db", Label: "Create database with owner…", Engine: "postgres", Danger: DangerMedium,
		Params: []Param{
			{Name: "db", Label: "Database name", Kind: ParamIdent},
			{Name: "owner", Label: "Owner (existing user)", Kind: ParamIdent},
		},
		Build: func(v map[string]string) Plan {
			return Plan{
				Title:   "Create database " + v["db"],
				Stmts:   []string{fmt.Sprintf("CREATE DATABASE %s OWNER %s", QuoteIdent(v["db"]), QuoteIdent(v["owner"]))},
				Danger:  DangerMedium,
				Summary: "creates database " + v["db"] + " owned by " + v["owner"],
			}
		},
	},
	{
		Key: "grant-all", Label: "Grant ALL on database…", Engine: "postgres", Danger: DangerMedium,
		Params: []Param{
			{Name: "db", Label: "Database", Kind: ParamIdent},
			{Name: "user", Label: "User", Kind: ParamIdent},
		},
		Build: func(v map[string]string) Plan {
			return Plan{
				Title:   "Grant ALL on " + v["db"] + " to " + v["user"],
				Stmts:   []string{fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE %s TO %s", QuoteIdent(v["db"]), QuoteIdent(v["user"]))},
				Danger:  DangerMedium,
				Summary: "full privileges on " + v["db"] + " for " + v["user"],
			}
		},
	},
	{
		Key: "grant-readonly", Label: "Grant read-only on schema…", Engine: "postgres", Danger: DangerMedium,
		Params: []Param{
			{Name: "schema", Label: "Schema", Kind: ParamIdent},
			{Name: "user", Label: "User", Kind: ParamIdent},
		},
		Build: func(v map[string]string) Plan {
			s, u := QuoteIdent(v["schema"]), QuoteIdent(v["user"])
			return Plan{
				Title: "Grant read-only on " + v["schema"] + " to " + v["user"],
				Stmts: []string{
					fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s", s, u),
					fmt.Sprintf("GRANT SELECT ON ALL TABLES IN SCHEMA %s TO %s", s, u),
					fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT SELECT ON TABLES TO %s", s, u),
				},
				Danger:  DangerMedium,
				Summary: "SELECT on every current and future table in " + v["schema"],
				Tx:      true,
			}
		},
	},
	{
		Key: "alter-password", Label: "Change user password…", Engine: "postgres", Danger: DangerMedium,
		Params: []Param{
			{Name: "user", Label: "User", Kind: ParamIdent},
			{Name: "password", Label: "New password", Kind: ParamSecret},
		},
		Build: func(v map[string]string) Plan {
			return Plan{
				Title:   "Change password for " + v["user"],
				Stmts:   []string{fmt.Sprintf("ALTER USER %s WITH PASSWORD %s", QuoteIdent(v["user"]), QuoteLiteral(v["password"]))},
				Danger:  DangerMedium,
				Summary: "replaces " + v["user"] + "'s password",
			}
		},
	},
	{
		Key: "drop-user", Label: "Drop user…", Engine: "postgres", Danger: DangerHigh,
		Params: []Param{
			{Name: "user", Label: "User to DROP", Kind: ParamIdent},
		},
		Build: func(v map[string]string) Plan {
			return Plan{
				Title:        "Drop user " + v["user"],
				Stmts:        []string{"DROP USER " + QuoteIdent(v["user"])},
				Danger:       DangerHigh,
				Summary:      "permanently removes role " + v["user"],
				ConfirmToken: v["user"],
			}
		},
	},
	{
		Key: "drop-db", Label: "Drop database…", Engine: "postgres", Danger: DangerHigh,
		Params: []Param{
			{Name: "db", Label: "Database to DROP", Kind: ParamIdent},
		},
		Build: func(v map[string]string) Plan {
			return Plan{
				Title:        "Drop database " + v["db"],
				Stmts:        []string{"DROP DATABASE " + QuoteIdent(v["db"])},
				Danger:       DangerHigh,
				Summary:      "PERMANENTLY DELETES database " + v["db"] + " and all its data",
				ConfirmToken: v["db"],
			}
		},
	},
}

// TemplatesFor filters the catalog to an engine.
func TemplatesFor(engine string) []Template {
	var out []Template
	for _, t := range Catalog {
		if t.Engine == "" || t.Engine == engine {
			out = append(out, t)
		}
	}
	return out
}

// MaintOp is one maintenance operation offered by the M key; built per
// node context rather than through the parameterized catalog.
type MaintOp struct {
	Label string
	Plan  Plan
}

// MaintenanceFor lists the operations applicable to an engine and an
// optional table (ref.Name == "" means whole-database scope).
func MaintenanceFor(engine string, ref ObjectRef) []MaintOp {
	var out []MaintOp
	table := ref.Name != ""
	q := func(s string) Plan {
		return Plan{Title: s, Stmts: []string{s}, Danger: DangerMedium, DB: ref.Database}
	}
	switch engine {
	case "sqlite":
		if table {
			out = append(out,
				MaintOp{"ANALYZE " + ref.Name, q("ANALYZE " + QuoteIdent(ref.Name))},
				MaintOp{"REINDEX " + ref.Name, q("REINDEX " + QuoteIdent(ref.Name))},
			)
		} else {
			out = append(out,
				MaintOp{"VACUUM", q("VACUUM")},
				MaintOp{"ANALYZE", q("ANALYZE")},
				MaintOp{"REINDEX", q("REINDEX")},
				MaintOp{"Integrity check", Plan{
					Title: "PRAGMA integrity_check", Stmts: []string{"PRAGMA integrity_check"},
					Danger: DangerNone, // pure read
				}},
			)
		}
	case "postgres":
		if table {
			t := qualifiedIdent(ref)
			out = append(out,
				MaintOp{"VACUUM (ANALYZE) " + ref.Name, q("VACUUM (ANALYZE) " + t)},
				MaintOp{"ANALYZE " + ref.Name, q("ANALYZE " + t)},
				MaintOp{"REINDEX TABLE " + ref.Name, q("REINDEX TABLE " + t)},
			)
		} else {
			out = append(out,
				MaintOp{"VACUUM (ANALYZE)", q("VACUUM (ANALYZE)")},
				MaintOp{"ANALYZE", q("ANALYZE")},
			)
		}
	}
	return out
}

func qualifiedIdent(ref ObjectRef) string {
	if ref.Schema == "" {
		return QuoteIdent(ref.Name)
	}
	return QuoteIdent(ref.Schema) + "." + QuoteIdent(ref.Name)
}
