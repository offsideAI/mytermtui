// Package dbx is mydb's engine-agnostic domain layer: the driver
// interface every database engine implements, and the tree node type the
// UI browses. Engine differences (schemas, roles, multiple databases…)
// are expressed as capability flags so the tree builder — not the
// drivers — decides what levels exist.
package dbx

import (
	"context"
	"time"
)

// Driver is one database engine (registered at startup).
type Driver interface {
	Engine() string // "sqlite" | "postgres"
	Capabilities() Capabilities
	Open(ctx context.Context, cfg ConnConfig) (Conn, error)
}

// Capabilities describes what an engine supports; the tree and menus
// adapt to these flags rather than switching on engine names.
type Capabilities struct {
	MultipleDatabases bool // postgres: true — sqlite: false (connection == file)
	Schemas           bool
	Roles             bool
	TransactionalDDL  bool
	ServerCancel      bool
}

// ConnConfig is a resolved connection: registry row plus secret.
type ConnConfig struct {
	Name     string
	Path     string // sqlite file
	Host     string
	Port     int
	DBName   string
	Username string
	Password string
	ReadOnly bool
}

// Conn is one open connection.
type Conn interface {
	Ping(ctx context.Context) error
	Close() error
	Introspector
	// ReadPage returns one page of a table/view with a stable ordering
	// chosen by the driver (PK → rowid/ctid → first column), so paging
	// never shuffles rows. Read-only by construction.
	ReadPage(ctx context.Context, ref ObjectRef, pr PageReq) (*Result, error)
}

// Introspector lists the objects the tree shows. Methods behind a false
// capability flag are never called for that engine.
type Introspector interface {
	// ServerInfo returns human-readable facts for the connection's Info
	// panel (engine version, file size, encoding…), in display order.
	ServerInfo(ctx context.Context) ([]KV, error)
	// Databases lists the server's databases (MultipleDatabases only).
	Databases(ctx context.Context) ([]DBInfo, error)
	// Schemas lists a database's schemas (Schemas only).
	Schemas(ctx context.Context, db string) ([]SchemaInfo, error)
	// Relations lists tables and views. db/schema are "" for engines
	// without those levels.
	Relations(ctx context.Context, db, schema string) ([]RelInfo, error)
	Columns(ctx context.Context, ref ObjectRef) ([]ColInfo, error)
	Indexes(ctx context.Context, ref ObjectRef) ([]IndexInfo, error)
	// Roles lists cluster roles (Roles only).
	Roles(ctx context.Context) ([]RoleInfo, error)
}

// ObjectRef names one database object unambiguously.
type ObjectRef struct {
	Database string
	Schema   string
	Name     string
}

type KV struct {
	Key   string
	Value string
}

type RelKind int

const (
	RelTable RelKind = iota
	RelView
)

type RelInfo struct {
	Name      string
	Kind      RelKind
	RowsEst   int64 // -1 when unknown
	SizeBytes int64 // -1 when unknown
}

type ColInfo struct {
	Name     string
	TypeName string
	NotNull  bool
	PK       bool
	Default  string
}

type IndexInfo struct {
	Name    string
	Unique  bool
	Columns []string
}

type DBInfo struct {
	Name      string
	Owner     string
	SizeBytes int64 // -1 when unknown
}

type SchemaInfo struct {
	Name  string
	Owner string
}

type RoleInfo struct {
	Name     string
	CanLogin bool
	Super    bool
}

// ConnStats is cheap per-connection metadata shown on the tree row.
type ConnStats struct {
	Version   string
	SizeBytes int64 // -1 when unknown
	Opened    time.Time
}
