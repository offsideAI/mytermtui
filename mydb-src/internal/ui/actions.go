package ui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/offsideai/mydb/internal/dbx"
)

// dispatch runs a keyboard action against the current cursor node.
func (m *Model) dispatch(act Action) tea.Cmd {
	switch act {
	case ActQuit:
		return tea.Quit
	case ActUp:
		m.moveCursor(-1)
	case ActDown:
		m.moveCursor(+1)
	case ActTop:
		m.cursor = 0
		m.clampCursor()
	case ActBottom:
		m.cursor = len(m.view) - 1
		m.clampCursor()
	case ActPageUp:
		m.moveCursor(-m.listHeight())
	case ActPageDown:
		m.moveCursor(+m.listHeight())

	case ActOpen:
		return m.toggleExpand()
	case ActExpand:
		if n := m.currentNode(); n != nil && !m.expanded[n.Path] {
			return m.toggleExpand()
		}
		m.moveCursor(+1) // Explorer style: → on an expanded node steps in
	case ActCollapse:
		return m.collapseOrParent()

	case ActFilter:
		m.filtering = true
		m.filterInput.SetValue(m.filterText)
		m.filterInput.CursorEnd()
		m.filterInput.Focus()

	case ActRefresh:
		return m.refresh()

	case ActConnect:
		if n := m.currentNode(); n != nil && n.Kind == dbx.KConnection {
			if m.state[n.ConnID] == connOpen {
				return m.note(levelInfo, "already connected")
			}
			return m.connect(*n)
		}
	case ActDisconnect:
		return m.disconnect()

	case ActNewConn:
		m.modal = newConnForm(m, nil)
	case ActEditConn:
		if c := m.cursorConn(); c != nil {
			conn := *c
			m.modal = newConnForm(m, &conn)
		}
	case ActDeleteConn:
		return m.confirmDeleteConn()
	case ActRevealSecret:
		return m.toggleReveal()

	case ActRunQuery:
		s := m.currentSQLSession(true)
		if s == nil {
			return m.note(levelWarn, "select a connection first")
		}
		m.panelOn = true
		m.focusRight = true
		m.tab = tabSQL
		return m.runSession(s, string(s.editor.Content()))
	case ActHistory:
		return m.openHistory()

	case ActCommands:
		return m.openCommands()
	case ActMaintenance:
		return m.openMaintenance()

	case ActSwapPane:
		return m.swapFocus()
	case ActTabNext:
		return m.switchTab(+1)
	case ActTabPrev:
		return m.switchTab(-1)

	case ActPanel:
		m.panelOn = !m.panelOn
		if !m.panelOn {
			m.focusRight = false
		}
	case ActPaneNarrow:
		if m.ratio > 0.15 {
			m.ratio -= 0.05
		}
	case ActPaneWiden:
		if m.ratio < 0.85 {
			m.ratio += 0.05
		}
	case ActHints:
		m.hintsOn = !m.hintsOn
		m.clampCursor()
	case ActHelp:
		m.modal = &HelpModal{}
	case ActMenu:
		m.menu.open = true
		m.menu.ii = firstItem(menus[m.menu.mi].items)
	}
	return nil
}

// cursorConn resolves the cursor row to its saved connection, walking up
// from deeper rows so E/X work anywhere inside a connection's subtree.
func (m *Model) cursorConn() *registryConn {
	n := m.currentNode()
	if n == nil || n.ConnID == 0 {
		return nil
	}
	if c, ok := m.connByID[n.ConnID]; ok {
		return &c
	}
	return nil
}

// toggleExpand expands or collapses the cursor node, connecting and
// fetching children as needed.
func (m *Model) toggleExpand() tea.Cmd {
	n := m.currentNode()
	if n == nil || !n.HasChildren {
		return nil
	}
	if m.expanded[n.Path] {
		m.expanded[n.Path] = false
		m.rebuildView(n.Path)
		return nil
	}
	m.expanded[n.Path] = true
	defer m.rebuildView(n.Path)

	if _, cached := m.childCache[n.Path]; cached {
		return nil
	}
	switch n.Kind {
	case dbx.KConnection:
		if m.state[n.ConnID] != connOpen {
			return m.connect(*n)
		}
		return m.fetchChildren(*n)
	case dbx.KTable, dbx.KView:
		m.childCache[n.Path] = tableSubgroups(*n)
	case dbx.KDatabase, dbx.KSchema, dbx.KGroup:
		return m.fetchChildren(*n)
	}
	return nil
}

// collapseOrParent is ←: collapse an expanded row, otherwise jump to the
// parent row.
func (m *Model) collapseOrParent() tea.Cmd {
	n := m.currentNode()
	if n == nil {
		return nil
	}
	if m.expanded[n.Path] {
		m.expanded[n.Path] = false
		m.rebuildView(n.Path)
		return nil
	}
	if pi := m.parentIndex(); pi >= 0 {
		m.cursor = pi
		m.clampCursor()
	}
	return nil
}

// connect opens a saved connection through its engine driver. Only one
// connection is active at a time (§3.2): any other open connection is
// disconnected first, releasing its server resources.
func (m *Model) connect(n dbx.Node) tea.Cmd {
	c, ok := m.connByID[n.ConnID]
	if !ok {
		return nil
	}
	drv, ok := m.drivers[c.Engine]
	if !ok {
		m.state[c.ID] = connFailed
		m.connErr[c.ID] = "engine not available yet"
		return m.note(levelWarn, c.Engine+" support arrives in a later milestone")
	}
	var cmds []tea.Cmd
	var others []int64
	for id := range m.open {
		if id != c.ID {
			others = append(others, id)
		}
	}
	for _, id := range others {
		if cmd := m.disconnectByID(id); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(others) > 0 {
		m.rebuildAnnex()
	}
	m.state[c.ID] = connOpening
	m.rebuildView("")
	cmds = append(cmds, openConnCmd(drv, c), m.ensureSpin())
	return tea.Batch(cmds...)
}

// disconnectByID silently closes one connection and forgets everything
// derived from it (subtree, Annex, Roles, grid; a running query is
// cancelled — the SQL buffer persists). Callers rebuild the view.
func (m *Model) disconnectByID(id int64) tea.Cmd {
	conn, ok := m.open[id]
	if !ok {
		return nil
	}
	c := m.connByID[id]
	delete(m.open, id)
	m.state[id] = connClosed
	delete(m.serverInfo, id)
	if m.grid.connID == id {
		m.grid = gridState{}
	}
	if s := m.sqlSessions[id]; s != nil {
		m.cancelSession(s)
	}
	delete(m.annexDBs, id)
	m.dropSubtree(dbx.ConnPath(c.Locality, id))
	m.dropSubtree(fmt.Sprintf("%s/conn:%d", dbx.SectionPath(annexLocality(c.Locality)), id))
	m.dropSubtree(rolesServerPath(id))
	return closeConnCmd(id, conn)
}

// disconnect closes the cursor row's connection (the d hotkey).
func (m *Model) disconnect() tea.Cmd {
	c := m.cursorConn()
	if c == nil {
		return nil
	}
	cmd := m.disconnectByID(c.ID)
	if cmd == nil {
		return nil
	}
	m.rebuildAnnex()
	m.rebuildView(dbx.ConnPath(c.Locality, c.ID))
	return tea.Batch(cmd, m.note(levelOK, "disconnected "+c.Name))
}

// refresh reloads the registry listing and re-fetches the schema of
// every open connection whose node is expanded.
func (m *Model) refresh() tea.Cmd {
	cmds := []tea.Cmd{loadConnsCmd(m.reg)}
	for id, conn := range m.open {
		c := m.connByID[id]
		path := dbx.ConnPath(c.Locality, id)
		prefix := path + "/"
		for k := range m.childCache {
			if len(k) > len(prefix) && k[:len(prefix)] == prefix || k == path {
				delete(m.childCache, k)
			}
		}
		if m.expanded[path] {
			c := m.connByID[id]
			cmds = append(cmds, fetchChildrenCmd(conn, m.capsFor(id), connNode(c)))
		}
		if m.capsFor(id).MultipleDatabases {
			cmds = append(cmds, discoverCmd(conn, id))
		}
		// Cached role lists refetch on next expand.
		delete(m.childCache, rolesServerPath(id))
	}
	cmds = append(cmds, m.note(levelInfo, "refreshing…"))
	return tea.Batch(cmds...)
}

// toggleReveal shows the cursor connection's saved password in the Info
// panel, auto-hiding after 10 seconds. A second press hides it early.
func (m *Model) toggleReveal() tea.Cmd {
	c := m.cursorConn()
	if c == nil {
		return nil
	}
	if m.revealID == c.ID {
		m.revealID = 0
		return nil
	}
	if c.Secret == "" {
		return m.note(levelInfo, "no password saved for "+c.Name)
	}
	m.revealID = c.ID
	m.revealSeq++
	seq := m.revealSeq
	m.panelOn = true // the reveal happens in the panel; make sure it shows
	return tea.Batch(
		m.note(levelWarn, "password visible in the panel — hides in 10s ("+m.keys.KeyFor(ActRevealSecret)+" hides now)"),
		tea.Tick(10*time.Second, func(time.Time) tea.Msg { return revealExpireMsg{seq} }),
	)
}

// confirmDeleteConn asks for the connection's name before deleting it —
// removing a saved connection also drops its query/job history.
func (m *Model) confirmDeleteConn() tea.Cmd {
	c := m.cursorConn()
	if c == nil {
		return nil
	}
	target := *c
	m.modal = newTypedConfirm(
		"Delete connection",
		[]string{
			"This deletes the saved connection and its history.",
			"The database itself is not touched.",
		},
		target.Name,
		func(m *Model) tea.Cmd {
			var cmds []tea.Cmd
			if conn, ok := m.open[target.ID]; ok {
				delete(m.open, target.ID)
				delete(m.state, target.ID)
				delete(m.serverInfo, target.ID)
				cmds = append(cmds, closeConnCmd(target.ID, conn))
			}
			if m.grid.connID == target.ID {
				m.grid = gridState{}
			}
			m.dropSubtree(dbx.ConnPath(target.Locality, target.ID))
			delete(m.annexDBs, target.ID)
			m.dropSubtree(fmt.Sprintf("%s/conn:%d", dbx.SectionPath(annexLocality(target.Locality)), target.ID))
			m.dropSubtree(rolesServerPath(target.ID))
			m.rebuildAnnex()
			cmds = append(cmds, regCmd("deleted "+target.Name, func() error {
				return m.reg.Delete(target.ID)
			}))
			return tea.Batch(cmds...)
		},
	)
	return nil
}
