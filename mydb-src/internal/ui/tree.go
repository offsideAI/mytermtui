package ui

import (
	"fmt"
	"strings"

	"github.com/offsideai/mydb/internal/dbx"
	"github.com/offsideai/mydb/internal/registry"
)

// Node construction: sections and connections come from the registry;
// everything deeper comes from a driver's Introspector. All child lists
// live in m.childCache keyed by the parent node's Path, so flatten is
// pure lookup.

// Annex sections hold databases discovered on connected servers; the
// plain sections hold the saved connections.
const (
	locLocal       = "local"
	locLocalAnnex  = "local-annex"
	locRemote      = "remote"
	locRemoteAnnex = "remote-annex"
)

func sectionNodes() []dbx.Node {
	mk := func(loc, name string) dbx.Node {
		return dbx.Node{
			Kind: dbx.KSection, Name: name, Path: dbx.SectionPath(loc),
			HasChildren: true, Meta: dbx.NodeMeta{Locality: loc},
		}
	}
	return []dbx.Node{
		mk(locLocal, "Local"),
		mk(locLocalAnnex, "Local (Annex)"),
		mk(locRemote, "Remote"),
		mk(locRemoteAnnex, "Remote (Annex)"),
	}
}

func annexLocality(loc string) string { return loc + "-annex" }

func isAnnex(loc string) bool { return strings.HasSuffix(loc, "-annex") }

func connNode(c registry.Connection) dbx.Node {
	target := c.Path
	ref := dbx.ObjectRef{}
	if c.Engine == "postgres" {
		target = fmt.Sprintf("%s:%d/%s", c.Host, c.Port, c.DBName)
		// The connection IS its configured database (§3.2): children are
		// that database's schemas, so the ref carries it.
		ref.Database = c.DBName
	}
	return dbx.Node{
		Kind: dbx.KConnection, Name: c.Name,
		Path: dbx.ConnPath(c.Locality, c.ID), HasChildren: true, ConnID: c.ID,
		Ref:  ref,
		Meta: dbx.NodeMeta{Engine: c.Engine, Locality: c.Locality, Target: target},
	}
}

// annexDBNode is one discovered sibling database in an Annex section.
func annexDBNode(sectionPath string, src registry.Connection, d dbx.DBInfo) dbx.Node {
	return dbx.Node{
		Kind: dbx.KDatabase, Name: d.Name,
		Path:        fmt.Sprintf("%s/conn:%d/db:%s", sectionPath, src.ID, d.Name),
		HasChildren: true, ConnID: src.ID,
		Ref:  dbx.ObjectRef{Database: d.Name},
		Meta: dbx.NodeMeta{Engine: src.Engine, SizeBytes: d.SizeBytes, Owner: d.Owner},
	}
}

// groupNodes builds a relation container's child groups plus the
// prebuilt table and view lists those groups expand into. parent is the
// connection (SQLite) or schema (Postgres) node; table refs inherit its
// database/schema coordinates.
func groupNodes(parent dbx.Node, rels []dbx.RelInfo) (groups, tables, views []dbx.Node) {
	tablesGroup := dbx.Node{
		Kind: dbx.KGroup, Group: dbx.GroupTables, Name: "Tables",
		Path: parent.ChildPath("g:tables"), ConnID: parent.ConnID, Ref: parent.Ref,
	}
	viewsGroup := dbx.Node{
		Kind: dbx.KGroup, Group: dbx.GroupViews, Name: "Views",
		Path: parent.ChildPath("g:views"), ConnID: parent.ConnID, Ref: parent.Ref,
	}
	for _, r := range rels {
		kind, grp := dbx.KTable, &tablesGroup
		if r.Kind == dbx.RelView {
			kind, grp = dbx.KView, &viewsGroup
		}
		n := dbx.Node{
			Kind: kind, Name: r.Name, Path: grp.ChildPath("t:" + r.Name),
			HasChildren: true, ConnID: parent.ConnID,
			Ref:  dbx.ObjectRef{Database: parent.Ref.Database, Schema: parent.Ref.Schema, Name: r.Name},
			Meta: dbx.NodeMeta{RowsEst: r.RowsEst, SizeBytes: r.SizeBytes},
		}
		if kind == dbx.KTable {
			tables = append(tables, n)
		} else {
			views = append(views, n)
		}
	}
	tablesGroup.Meta.Count = len(tables)
	tablesGroup.HasChildren = len(tables) > 0
	viewsGroup.Meta.Count = len(views)
	viewsGroup.HasChildren = len(views) > 0
	return []dbx.Node{tablesGroup, viewsGroup}, tables, views
}

func schemaNodes(db dbx.Node, schemas []dbx.SchemaInfo) []dbx.Node {
	out := make([]dbx.Node, 0, len(schemas))
	for _, s := range schemas {
		out = append(out, dbx.Node{
			Kind: dbx.KSchema, Name: s.Name, Path: db.ChildPath("sch:" + s.Name),
			HasChildren: true, ConnID: db.ConnID,
			Ref:  dbx.ObjectRef{Database: db.Ref.Database, Schema: s.Name},
			Meta: dbx.NodeMeta{Owner: s.Owner},
		})
	}
	return out
}

func rolesGroup(conn dbx.Node) dbx.Node {
	return dbx.Node{
		Kind: dbx.KGroup, Group: dbx.GroupRoles, Name: "Roles",
		Path: conn.ChildPath("g:roles"), HasChildren: true, ConnID: conn.ConnID,
		Meta: dbx.NodeMeta{Count: -1},
	}
}

func roleNodes(group dbx.Node, roles []dbx.RoleInfo) []dbx.Node {
	out := make([]dbx.Node, 0, len(roles))
	for _, r := range roles {
		out = append(out, dbx.Node{
			Kind: dbx.KRole, Name: r.Name, Path: group.ChildPath("r:" + r.Name),
			ConnID: group.ConnID,
			Meta:   dbx.NodeMeta{CanLogin: r.CanLogin, Super: r.Super},
		})
	}
	return out
}

// tableSubgroups is a table/view's fixed children: Columns (and, for
// tables, Indexes). Their own children load on demand.
func tableSubgroups(t dbx.Node) []dbx.Node {
	out := []dbx.Node{{
		Kind: dbx.KGroup, Group: dbx.GroupColumns, Name: "Columns",
		Path: t.ChildPath("g:cols"), HasChildren: true, ConnID: t.ConnID,
		Ref: t.Ref, Meta: dbx.NodeMeta{Count: -1},
	}}
	if t.Kind == dbx.KTable {
		out = append(out, dbx.Node{
			Kind: dbx.KGroup, Group: dbx.GroupIndexes, Name: "Indexes",
			Path: t.ChildPath("g:idx"), HasChildren: true, ConnID: t.ConnID,
			Ref: t.Ref, Meta: dbx.NodeMeta{Count: -1},
		})
	}
	return out
}

func columnNodes(parent dbx.Node, cols []dbx.ColInfo) []dbx.Node {
	out := make([]dbx.Node, 0, len(cols))
	for _, c := range cols {
		out = append(out, dbx.Node{
			Kind: dbx.KColumn, Name: c.Name, Path: parent.ChildPath("c:" + c.Name),
			ConnID: parent.ConnID, Ref: parent.Ref,
			Meta: dbx.NodeMeta{TypeName: c.TypeName, NotNull: c.NotNull, PK: c.PK},
		})
	}
	return out
}

func indexNodes(parent dbx.Node, idxs []dbx.IndexInfo) []dbx.Node {
	out := make([]dbx.Node, 0, len(idxs))
	for _, ix := range idxs {
		out = append(out, dbx.Node{
			Kind: dbx.KIndex, Name: ix.Name, Path: parent.ChildPath("i:" + ix.Name),
			ConnID: parent.ConnID, Ref: parent.Ref,
			Meta: dbx.NodeMeta{Unique: ix.Unique, Columns: strings.Join(ix.Columns, ", ")},
		})
	}
	return out
}

// --- flatten -------------------------------------------------------------

// rebuildView re-flattens the tree and restores the cursor onto keepPath
// (or the previous cursor node) where possible.
func (m *Model) rebuildView(keepPath string) {
	if keepPath == "" {
		keepPath = m.cursorPath()
	}
	m.flatten()
	m.cursor = 0
	if keepPath != "" {
		for vi, ni := range m.view {
			if m.nodes[ni].Path == keepPath {
				m.cursor = vi
				break
			}
		}
	}
	m.clampCursor()
}

// flatten materializes the visible tree: sections, then each expanded
// node's cached children inlined beneath it at increasing depth. With a
// filter active, a row survives if its own name matches or any visible
// descendant does — ancestors stay as context for their matches.
func (m *Model) flatten() {
	needle := strings.ToLower(m.filterText)
	var out []dbx.Node
	var walk func(list []dbx.Node, depth int) bool
	walk = func(list []dbx.Node, depth int) bool {
		any := false
		for _, n := range list {
			match := needle == "" || strings.Contains(strings.ToLower(n.Name), needle)
			n.Depth = depth
			out = append(out, n)
			idx := len(out) - 1
			kidsEmitted := false
			if m.expanded[n.Path] {
				if kids, ok := m.childCache[n.Path]; ok {
					kidsEmitted = walk(kids, depth+1)
				}
			}
			if !match && !kidsEmitted {
				out = out[:idx] // no children were added: retract just this row
				continue
			}
			any = true
		}
		return any
	}
	walk(m.roots, 0)
	m.nodes = out
	m.view = make([]int, len(out))
	for i := range out {
		m.view[i] = i
	}
}

func (m *Model) currentNode() *dbx.Node {
	if m.cursor < 0 || m.cursor >= len(m.view) {
		return nil
	}
	ni := m.view[m.cursor]
	if ni < 0 || ni >= len(m.nodes) {
		return nil
	}
	return &m.nodes[ni]
}

func (m *Model) cursorPath() string {
	if n := m.currentNode(); n != nil {
		return n.Path
	}
	return ""
}

// parentIndex is the view index of the current node's parent row (the
// nearest prior row with a smaller depth), or -1. Depth-based so node
// names containing path separators cannot confuse it.
func (m *Model) parentIndex() int {
	n := m.currentNode()
	if n == nil {
		return -1
	}
	for vi := m.cursor - 1; vi >= 0; vi-- {
		if m.nodes[m.view[vi]].Depth < n.Depth {
			return vi
		}
	}
	return -1
}

// dropSubtree forgets every cached child list and expansion under (and
// including) path — used on disconnect and refresh.
func (m *Model) dropSubtree(path string) {
	prefix := path + "/"
	for k := range m.childCache {
		if k == path || strings.HasPrefix(k, prefix) {
			delete(m.childCache, k)
		}
	}
	for k := range m.expanded {
		if k == path || strings.HasPrefix(k, prefix) {
			delete(m.expanded, k)
		}
	}
}

// crumbs renders the ancestor chain of the selected row for the
// breadcrumb line: "prod-pg ▸ Tables ▸ users".
func (m *Model) crumbs() string {
	n := m.currentNode()
	if n == nil {
		return "mydb"
	}
	parts := []string{n.Name}
	depth := n.Depth
	for vi := m.cursor - 1; vi >= 0 && depth > 0; vi-- {
		cand := m.nodes[m.view[vi]]
		if cand.Depth < depth {
			parts = append([]string{cand.Name}, parts...)
			depth = cand.Depth
		}
	}
	return strings.Join(parts, " ▸ ")
}
