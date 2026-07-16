package dbx

import "fmt"

// NodeKind classifies a tree row. Rendering (columns, colors) and child
// fetching switch on it.
type NodeKind int

const (
	KSection NodeKind = iota // Local / Remote
	KConnection
	KDatabase // postgres only
	KSchema   // postgres only
	KGroup    // "Tables (12)", "Views (2)", "Columns", "Indexes"
	KTable
	KView
	KIndex
	KColumn
	KRole
)

// GroupKind says what a KGroup node expands into.
type GroupKind int

const (
	GroupNone GroupKind = iota
	GroupTables
	GroupViews
	GroupColumns
	GroupIndexes
	GroupRoles
)

// Node is one row of the browse tree (replaces the file browsers'
// fsx.Entry). Path is a stable identity that survives rebuilds — it keys
// the expanded set and the child cache.
type Node struct {
	Kind        NodeKind
	Group       GroupKind // set when Kind == KGroup
	Name        string
	Path        string
	Depth       int // display depth, assigned by flatten
	HasChildren bool
	ConnID      int64     // 0 for sections
	Ref         ObjectRef // the object this node belongs to (tables, columns…)
	Meta        NodeMeta
}

// NodeMeta carries per-kind display data; only the fields relevant to
// the node's kind are set.
type NodeMeta struct {
	Engine    string // connection
	Locality  string // connection: "local" | "remote"
	Target    string // connection: path or host:port/db
	RowsEst   int64  // table/view; -1 unknown
	SizeBytes int64  // table/view/connection; -1 unknown
	TypeName  string // column
	NotNull   bool   // column
	PK        bool   // column
	Unique    bool   // index
	Columns   string // index: backing columns
	Count     int    // group: child count (-1 until loaded)
	CanLogin  bool   // role
	Super     bool   // role
	Owner     string // database/schema
}

// Child-path builders: every node's Path embeds its parent's, so paths
// are unique and stable across reloads.

func SectionPath(locality string) string { return "sec:" + locality }

func ConnPath(locality string, id int64) string {
	return fmt.Sprintf("%s/conn:%d", SectionPath(locality), id)
}

func (n Node) ChildPath(part string) string { return n.Path + "/" + part }
