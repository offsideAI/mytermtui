// Package ui implements the Bubble Tea program: an Elm-architecture
// model whose update loop never touches a database or the network —
// all I/O runs in commands and returns as messages.
package ui

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/offsideai/mydb/internal/config"
	"github.com/offsideai/mydb/internal/dbx"
	pgdrv "github.com/offsideai/mydb/internal/drivers/postgres"
	sqlitedrv "github.com/offsideai/mydb/internal/drivers/sqlite"
	"github.com/offsideai/mydb/internal/jobs"
	"github.com/offsideai/mydb/internal/registry"
)

// ioTimeout bounds every M1 introspection call; nothing here should be
// slow, and a hung file system must not wedge a command forever.
const ioTimeout = 30 * time.Second

type statusLevel int

const (
	levelInfo statusLevel = iota
	levelOK
	levelWarn
	levelErr
)

type statusNote struct {
	text  string
	level statusLevel
	id    int
}

// connState tracks each saved connection's lifecycle.
type connState int

const (
	connClosed connState = iota
	connOpening
	connOpen
	connFailed
)

// registryConn is a local alias so ui code can pass connections around
// without spelling the package path everywhere.
type registryConn = registry.Connection

// Model is the root Bubble Tea model.
type Model struct {
	cfg     config.Config
	theme   Theme
	keys    Keymap
	reg     *registry.Registry
	drivers map[string]dbx.Driver

	w, h  int
	ready bool
	home  string

	// Registry + live connections.
	conns      []registry.Connection
	connByID   map[int64]registry.Connection
	open       map[int64]dbx.Conn
	state      map[int64]connState
	connErr    map[int64]string
	serverInfo map[int64][]dbx.KV
	annexDBs   map[int64][]dbx.DBInfo // source connection → discovered databases

	// Tree state (see tree.go).
	roots      []dbx.Node
	expanded   map[string]bool
	childCache map[string][]dbx.Node
	nodes      []dbx.Node // flattened, display order
	view       []int      // indexes into nodes
	cursor     int
	offset     int

	filterText  string
	filterInput textinput.Model
	filtering   bool

	panelOn    bool
	ratio      float64
	hintsOn    bool
	focusRight bool // keys route to the workspace panel
	tab        int  // active workspace tab (tabInfo/tabData/tabSQL)
	grid       gridState

	sqlSessions map[int64]*sqlSession // per-connection SQL tabs
	qSeq        int                   // query id sequence

	jobsQ       *jobs.Queue
	jobsTicking bool
	jobCursor   int
	jobNote     tea.Cmd // pending status note from a finished job

	spinning bool // a spinner tick is scheduled
	tickN    int

	menu  menuState
	modal Modal

	revealID  int64 // connection whose password is shown in the panel
	revealSeq int   // invalidates stale auto-hide ticks

	status    statusNote
	statusSeq int
}

// New builds the model.
func New(cfg config.Config, reg *registry.Registry) *Model {
	fi := textinput.New()
	fi.Prompt = "filter: "
	fi.CharLimit = 256
	home, _ := os.UserHomeDir()
	m := &Model{
		cfg:   cfg,
		home:  home,
		theme: NewTheme(cfg.Theme.Name),
		keys:  BuildKeymap(cfg.Keys),
		reg:   reg,
		drivers: map[string]dbx.Driver{
			"sqlite":   sqlitedrv.New(),
			"postgres": pgdrv.New(),
		},
		connByID:    map[int64]registry.Connection{},
		open:        map[int64]dbx.Conn{},
		state:       map[int64]connState{},
		connErr:     map[int64]string{},
		serverInfo:  map[int64][]dbx.KV{},
		annexDBs:    map[int64][]dbx.DBInfo{},
		sqlSessions: map[int64]*sqlSession{},
		roots:       sectionNodes(),
		expanded:    map[string]bool{},
		childCache:  map[string][]dbx.Node{},
		panelOn:     cfg.General.ShowPanel,
		ratio:       cfg.General.SplitRatio,
		hintsOn:     cfg.General.ShowHints,
		filterInput: fi,
	}
	// Sections start expanded: an empty tree at launch reads as broken.
	for _, s := range m.roots {
		m.expanded[s.Path] = true
	}
	m.jobsQ = jobs.New(2, m.onJobDone)
	return m
}

func (m *Model) Init() tea.Cmd {
	return loadConnsCmd(m.reg)
}

// InstallDriver overrides an engine's driver — used by the screenshot
// harness and tests to substitute the in-memory fake.
func (m *Model) InstallDriver(engine string, d dbx.Driver) {
	m.drivers[engine] = d
}

// --- messages ----------------------------------------------------------

type connsLoadedMsg struct {
	conns []registry.Connection
	err   error
}

type connOpenedMsg struct {
	id   int64
	conn dbx.Conn
	info []dbx.KV
	err  error
}

// childrenLoadedMsg delivers a node's children. extra carries prebuilt
// grandchild lists (a schema's Tables/Views groups arrive with their
// contents) so one introspection call fills several cache levels.
type childrenLoadedMsg struct {
	path  string
	nodes []dbx.Node
	extra map[string][]dbx.Node
	err   error
}

// regDoneMsg reports a registry mutation (create/update/delete).
type regDoneMsg struct {
	desc string
	err  error
}

type connClosedMsg struct{ id int64 }

type statusExpireMsg struct{ id int }

type revealExpireMsg struct{ seq int }

// annexLoadedMsg delivers a connected server's database listing for the
// Annex sections.
type annexLoadedMsg struct {
	id  int64
	dbs []dbx.DBInfo
	err error
}

type spinTickMsg struct{}

// --- commands ----------------------------------------------------------

func loadConnsCmd(reg *registry.Registry) tea.Cmd {
	return func() tea.Msg {
		conns, err := reg.Connections()
		return connsLoadedMsg{conns: conns, err: err}
	}
}

func openConnCmd(drv dbx.Driver, c registry.Connection) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), ioTimeout)
		defer cancel()
		conn, err := drv.Open(ctx, dbx.ConnConfig{
			Name: c.Name, Path: config.ExpandTilde(c.Path),
			Host: c.Host, Port: c.Port, DBName: c.DBName,
			Username: c.Username, Password: c.Secret,
			ReadOnly: c.ReadOnly(),
		})
		if err != nil {
			return connOpenedMsg{id: c.ID, err: err}
		}
		info, err := conn.ServerInfo(ctx)
		if err != nil {
			info = nil // non-fatal: the panel just shows less
		}
		return connOpenedMsg{id: c.ID, conn: conn, info: info}
	}
}

// fetchChildrenCmd loads a node's children through the driver; the tree
// builder — not the driver — decides the levels via capability flags.
func fetchChildrenCmd(conn dbx.Conn, caps dbx.Capabilities, n dbx.Node) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), ioTimeout)
		defer cancel()
		fail := func(err error) tea.Msg { return childrenLoadedMsg{path: n.Path, err: err} }

		relGroups := func(db, schema string) tea.Msg {
			rels, err := conn.Relations(ctx, db, schema)
			if err != nil {
				return fail(err)
			}
			groups, tables, views := groupNodes(n, rels)
			return childrenLoadedMsg{path: n.Path, nodes: groups, extra: map[string][]dbx.Node{
				groups[0].Path: tables,
				groups[1].Path: views,
			}}
		}

		switch n.Kind {
		case dbx.KConnection:
			// A saved connection scopes to its own database (§3.2): its
			// children are that database's schemas — sibling databases go
			// to the Annex sections and cluster roles to the Roles section.
			if !caps.Schemas {
				return relGroups("", "")
			}
			schemas, err := conn.Schemas(ctx, n.Ref.Database)
			if err != nil {
				return fail(err)
			}
			return childrenLoadedMsg{path: n.Path, nodes: schemaNodes(n, schemas)}
		case dbx.KDatabase:
			schemas, err := conn.Schemas(ctx, n.Ref.Database)
			if err != nil {
				return fail(err)
			}
			return childrenLoadedMsg{path: n.Path, nodes: schemaNodes(n, schemas)}
		case dbx.KSchema:
			return relGroups(n.Ref.Database, n.Ref.Schema)
		case dbx.KGroup:
			switch n.Group {
			case dbx.GroupColumns:
				cols, err := conn.Columns(ctx, n.Ref)
				if err != nil {
					return fail(err)
				}
				return childrenLoadedMsg{path: n.Path, nodes: columnNodes(n, cols)}
			case dbx.GroupIndexes:
				idxs, err := conn.Indexes(ctx, n.Ref)
				if err != nil {
					return fail(err)
				}
				return childrenLoadedMsg{path: n.Path, nodes: indexNodes(n, idxs)}
			case dbx.GroupRoles:
				roles, err := conn.Roles(ctx)
				if err != nil {
					return fail(err)
				}
				return childrenLoadedMsg{path: n.Path, nodes: roleNodes(n, roles)}
			}
		}
		return childrenLoadedMsg{path: n.Path}
	}
}

// discoverCmd enumerates a connected server's databases for the Annex.
func discoverCmd(conn dbx.Conn, id int64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), ioTimeout)
		defer cancel()
		dbs, err := conn.Databases(ctx)
		return annexLoadedMsg{id: id, dbs: dbs, err: err}
	}
}

func regCmd(desc string, fn func() error) tea.Cmd {
	return func() tea.Msg {
		return regDoneMsg{desc: desc, err: fn()}
	}
}

func closeConnCmd(id int64, conn dbx.Conn) tea.Cmd {
	return func() tea.Msg {
		conn.Close()
		return connClosedMsg{id: id}
	}
}

func spinTickCmd() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg { return spinTickMsg{} })
}

func (m *Model) note(level statusLevel, text string) tea.Cmd {
	m.statusSeq++
	m.status = statusNote{text: text, level: level, id: m.statusSeq}
	id := m.statusSeq
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return statusExpireMsg{id} })
}

// ensureSpin keeps one spinner tick scheduled while a connect or a
// query is live.
func (m *Model) ensureSpin() tea.Cmd {
	if m.spinning {
		return nil
	}
	busy := false
	for _, st := range m.state {
		if st == connOpening {
			busy = true
		}
	}
	for _, s := range m.sqlSessions {
		if s.running {
			busy = true
		}
	}
	if busy {
		m.spinning = true
		return spinTickCmd()
	}
	return nil
}

// --- update ------------------------------------------------------------

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.ready = true
		m.clampCursor()
		return m, nil

	case tea.KeyMsg:
		return m.updateKey(msg)

	case connsLoadedMsg:
		if msg.err != nil {
			return m, m.note(levelErr, "registry: "+msg.err.Error())
		}
		m.absorbConns(msg.conns)
		m.rebuildView("")
		return m, nil

	case connOpenedMsg:
		if _, exists := m.connByID[msg.id]; !exists {
			// Deleted while connecting: close the orphan and move on.
			if msg.conn != nil {
				return m, closeConnCmd(msg.id, msg.conn)
			}
			return m, nil
		}
		if msg.err != nil {
			m.state[msg.id] = connFailed
			m.connErr[msg.id] = msg.err.Error()
			m.rebuildView("")
			return m, m.note(levelErr, "connect: "+msg.err.Error())
		}
		m.state[msg.id] = connOpen
		m.open[msg.id] = msg.conn
		m.serverInfo[msg.id] = msg.info
		delete(m.connErr, msg.id)
		c := m.connByID[msg.id]
		m.rebuildRolesSection()
		m.rebuildView("")
		cmds := []tea.Cmd{
			fetchChildrenCmd(msg.conn, m.capsFor(msg.id), connNode(c)),
			regCmd("", func() error { return m.reg.TouchUsed(msg.id) }),
		}
		if m.capsFor(msg.id).MultipleDatabases {
			cmds = append(cmds, discoverCmd(msg.conn, msg.id))
		}
		return m, tea.Batch(cmds...)

	case childrenLoadedMsg:
		if msg.err != nil {
			delete(m.expanded, msg.path)
			m.rebuildView("")
			return m, m.note(levelErr, "schema: "+msg.err.Error())
		}
		m.childCache[msg.path] = msg.nodes
		for k, v := range msg.extra {
			m.childCache[k] = v
		}
		m.rebuildView("")
		return m, nil

	case pageLoadedMsg:
		if msg.path != m.grid.path {
			return m, nil // grid rebound before the page arrived
		}
		m.grid.loading = false
		if msg.err != nil {
			m.grid.err = msg.err.Error()
			return m, nil
		}
		m.grid.err = ""
		m.grid.setResult(msg.res)
		m.grid.page = msg.page
		return m, nil

	case copyDoneMsg:
		if msg.err != nil {
			return m, m.note(levelErr, "copy: "+msg.err.Error())
		}
		return m, m.note(levelOK, "copied "+msg.what)

	case queryDoneMsg:
		return m, m.absorbQueryDone(msg)

	case adminDoneMsg:
		return m, m.absorbAdminDone(msg)

	case jobsTickMsg:
		m.jobsQ.Tick()
		m.tickN++
		var cmds []tea.Cmd
		if note := m.jobNote; note != nil {
			m.jobNote = nil
			cmds = append(cmds, note)
		}
		if m.jobsQ.HasActive() {
			cmds = append(cmds, m.jobsTickCmd())
		} else {
			m.jobsTicking = false
		}
		return m, tea.Batch(cmds...)

	case historyLoadedMsg:
		if hm, ok := m.modal.(*HistoryModal); ok {
			hm.absorb(msg)
		}
		return m, nil

	case regDoneMsg:
		if msg.err != nil {
			return m, m.note(levelErr, msg.desc+": "+msg.err.Error())
		}
		if msg.desc == "" { // silent bookkeeping (last_used_at)
			return m, nil
		}
		return m, tea.Batch(m.note(levelOK, msg.desc), loadConnsCmd(m.reg))

	case annexLoadedMsg:
		if msg.err != nil {
			return m, m.note(levelWarn, "discovery: "+msg.err.Error())
		}
		if _, exists := m.connByID[msg.id]; !exists {
			return m, nil // deleted while discovering
		}
		m.annexDBs[msg.id] = msg.dbs
		m.rebuildAnnex()
		m.rebuildView("")
		return m, nil

	case connClosedMsg:
		return m, nil

	case spinTickMsg:
		m.spinning = false
		m.tickN++
		return m, m.ensureSpin()

	case statusExpireMsg:
		if m.status.id == msg.id {
			m.status = statusNote{}
		}
		return m, nil

	case revealExpireMsg:
		if m.revealSeq == msg.seq {
			m.revealID = 0
		}
		return m, nil
	}
	return m, nil
}

// absorbConns installs a fresh registry listing: connection nodes are
// rebuilt, open handles for deleted connections are dropped, and caches
// for surviving connections are kept.
func (m *Model) absorbConns(conns []registry.Connection) {
	m.conns = conns
	seen := map[int64]bool{}
	byLoc := map[string][]dbx.Node{}
	m.connByID = map[int64]registry.Connection{}
	for _, c := range conns {
		seen[c.ID] = true
		m.connByID[c.ID] = c
		byLoc[c.Locality] = append(byLoc[c.Locality], connNode(c))
	}
	for _, s := range m.roots {
		m.childCache[s.Path] = byLoc[s.Meta.Locality]
	}
	for id, conn := range m.open {
		if !seen[id] {
			go conn.Close() // fire-and-forget: nothing depends on the result
			delete(m.open, id)
			delete(m.state, id)
			delete(m.connErr, id)
			delete(m.serverInfo, id)
		}
	}
	for id := range m.annexDBs {
		if !seen[id] {
			delete(m.annexDBs, id)
		}
	}
	for id, s := range m.sqlSessions {
		if !seen[id] {
			m.cancelSession(s)
			delete(m.sqlSessions, id)
		}
	}
	m.rebuildAnnex()
	if m.revealID != 0 && !seen[m.revealID] {
		m.revealID = 0 // never keep showing a deleted connection's password
	}
	// A connection that moved sections leaves stale subtree keys behind;
	// they are keyed by conn id inside the path, so they simply become
	// unreachable and are dropped on disconnect/refresh.
}

// rebuildAnnex recomputes the Annex sections' child lists from the
// discovered databases: flat, sorted, deduplicated across connections to
// the same server, excluding databases that already have their own saved
// connection (those live in Local/Remote proper) and each source
// connection's own database (the connection node *is* that database).
func (m *Model) rebuildAnnex() {
	for _, loc := range []string{locLocal, locRemote} {
		secPath := dbx.SectionPath(annexLocality(loc))
		var nodes []dbx.Node
		seen := map[string]bool{}
		for _, c := range m.conns {
			if c.Locality != loc {
				continue
			}
			for _, d := range m.annexDBs[c.ID] {
				key := fmt.Sprintf("%s:%d/%s", c.Host, c.Port, d.Name)
				if seen[key] || m.hasSavedConn(c.Host, c.Port, d.Name) {
					continue
				}
				seen[key] = true
				nodes = append(nodes, annexDBNode(secPath, c, d))
			}
		}
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
		m.childCache[secPath] = nodes
	}
	m.rebuildRolesSection()
}

// rebuildRolesSection recomputes the top-level Roles section: one entry
// per CONNECTED roles-capable server, deduplicated by host:port (the
// first saved connection to a server labels it).
func (m *Model) rebuildRolesSection() {
	var nodes []dbx.Node
	seen := map[string]bool{}
	for _, c := range m.conns {
		if _, open := m.open[c.ID]; !open || !m.capsFor(c.ID).Roles {
			continue
		}
		key := fmt.Sprintf("%s:%d", c.Host, c.Port)
		if seen[key] {
			continue
		}
		seen[key] = true
		nodes = append(nodes, rolesServerNode(c))
	}
	m.childCache[dbx.SectionPath(locRoles)] = nodes
}

// hasSavedConn reports whether a server database is already represented
// by a saved connection.
func (m *Model) hasSavedConn(host string, port int, db string) bool {
	for _, c := range m.conns {
		if c.Engine != "sqlite" && c.Host == host && c.Port == port && c.DBName == db {
			return true
		}
	}
	return false
}

// capsFor returns the engine capabilities for a saved connection.
func (m *Model) capsFor(id int64) dbx.Capabilities {
	if c, ok := m.connByID[id]; ok {
		if d, ok := m.drivers[c.Engine]; ok {
			return d.Capabilities()
		}
	}
	return dbx.Capabilities{}
}

// fetchChildren dispatches an async child load for a node, guarding on
// an open connection.
func (m *Model) fetchChildren(n dbx.Node) tea.Cmd {
	conn := m.open[n.ConnID]
	if conn == nil {
		delete(m.expanded, n.Path)
		return m.note(levelWarn, "not connected")
	}
	return fetchChildrenCmd(conn, m.capsFor(n.ConnID), n)
}

// --- key routing ---------------------------------------------------------

func (m *Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Modal captures everything.
	if m.modal != nil {
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Update(m, msg)
		return m, cmd
	}

	// Menu bar navigation.
	if m.menu.open {
		return m, m.updateMenu(key)
	}

	// Workspace focus: keys go to the panel until tab/esc returns.
	if m.focusRight {
		return m, m.workspaceKey(msg)
	}

	// Filter input mode.
	if m.filtering {
		switch key {
		case "esc":
			m.filtering = false
			m.filterText = ""
			m.filterInput.SetValue("")
			m.rebuildView(m.cursorPath())
			return m, nil
		case "enter":
			m.filtering = false
			return m, nil
		default:
			var cmd tea.Cmd
			m.filterInput, cmd = m.filterInput.Update(msg)
			m.filterText = m.filterInput.Value()
			m.rebuildView("")
			return m, cmd
		}
	}

	if key == "esc" {
		if m.filterText != "" {
			m.filterText = ""
			m.filterInput.SetValue("")
			m.rebuildView(m.cursorPath())
		}
		return m, nil
	}

	if act, ok := m.keys.Lookup(key); ok {
		return m, m.dispatch(act)
	}
	return m, nil
}

// --- view-state helpers ----------------------------------------------------

func (m *Model) listHeight() int {
	h := m.h - 4 - m.hintBarH() // menubar, breadcrumb, header, status bar
	if m.filtering || m.filterText != "" {
		h--
	}
	if m.jobBarVisible() {
		h--
	}
	if h < 1 {
		h = 1
	}
	return h
}

// hintBarH is the height of the nano-style shortcut box: two hint rows
// plus the box border. Auto-hidden on very short terminals.
func (m *Model) hintBarH() int {
	if !m.hintsOn || m.h < 16 {
		return 0
	}
	return 4
}

func (m *Model) clampCursor() {
	if m.cursor >= len(m.view) {
		m.cursor = len(m.view) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	lh := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+lh {
		m.offset = m.cursor - lh + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *Model) moveCursor(delta int) {
	if len(m.view) == 0 {
		return
	}
	m.cursor += delta
	m.clampCursor()
}
