package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/offsideai/mydb/internal/dbx"
)

func (m *Model) View() string {
	if !m.ready {
		return ""
	}
	var out []string
	out = append(out, m.renderMenuBar())
	out = append(out, m.renderMain()...)
	if m.filtering || m.filterText != "" {
		out = append(out, m.renderFilterLine())
	}
	if m.hintBarH() > 0 {
		out = append(out, m.renderHintBar()...)
	}
	out = append(out, m.renderStatusBar())
	return strings.Join(out, "\n")
}

// --- menu bar ---------------------------------------------------------------

func (m *Model) renderMenuBar() string {
	t := m.theme
	var b strings.Builder
	for i, def := range menus {
		if m.menu.open && i == m.menu.mi {
			b.WriteString(t.MenuTitleOn.Render(def.title))
		} else {
			b.WriteString(t.MenuTitle.Render(def.title))
		}
	}
	left := b.String()
	title := " mydb "
	gap := m.w - lipgloss.Width(left) - lipgloss.Width(title)
	if gap < 0 {
		gap = 0
	}
	return left + t.MenuBar.Render(strings.Repeat(" ", gap)) + t.MenuBar.Render(title)
}

// menuXOffset is the column where menu mi's dropdown should start.
func menuXOffset(mi int) int {
	x := 0
	for i := 0; i < mi && i < len(menus); i++ {
		x += lipgloss.Width(menus[i].title) + 2 // MenuTitle pads 1 each side
	}
	return x
}

// overlayMenu draws the open dropdown over the top of the main block.
func (m *Model) overlayMenu(lines []string) {
	t := m.theme
	def := menus[m.menu.mi]

	labelW := 0
	for _, it := range def.items {
		if w := lipgloss.Width(it.label); w > labelW {
			labelW = w
		}
	}
	keyW := 0
	for _, it := range def.items {
		if w := lipgloss.Width(m.keys.KeyFor(it.action)); w > keyW {
			keyW = w
		}
	}
	var rows []string
	for i, it := range def.items {
		if it.sep {
			rows = append(rows, t.MenuKey.Render(strings.Repeat("─", labelW+keyW+3)))
			continue
		}
		line := pad(it.label, labelW) + "   " + t.MenuKey.Render(padLeft(m.keys.KeyFor(it.action), keyW))
		if i == m.menu.ii {
			line = t.MenuItemOn.Render(pad(it.label, labelW) + "   " + padLeft(m.keys.KeyFor(it.action), keyW))
		}
		rows = append(rows, line)
	}
	box := t.MenuBox.Render(strings.Join(rows, "\n"))
	boxLines := strings.Split(box, "\n")
	xoff := menuXOffset(m.menu.mi)
	if bw := lipgloss.Width(boxLines[0]); xoff+bw > m.w {
		xoff = m.w - bw
		if xoff < 0 {
			xoff = 0
		}
	}
	for i := 0; i < len(boxLines) && i < len(lines); i++ {
		over := strings.Repeat(" ", xoff) + boxLines[i]
		gap := m.w - lipgloss.Width(over)
		if gap > 0 {
			over += strings.Repeat(" ", gap)
		}
		lines[i] = over
	}
}

// --- main block: breadcrumb + header + tree rows (+ info panel, menu, modal) --

func (m *Model) renderMain() []string {
	listH := m.listHeight()
	var lines []string

	showRight := m.panelOn && m.w >= 70
	if showRight {
		lw := int(float64(m.w) * m.ratio)
		if lw < 24 {
			lw = 24
		}
		if lw > m.w-25 {
			lw = m.w - 25
		}
		rw := m.w - lw - 1

		left := m.treeLines(lw, listH)
		right := m.renderWorkspace(rw, listH+2)
		div := m.theme.PanelMeta.Render("│")
		lines = make([]string, listH+2)
		for i := range lines {
			lines[i] = left[i] + div + right[i]
		}
	} else {
		lines = m.treeLines(m.w, listH)
	}

	if m.modal != nil {
		content := m.theme.ModalBox.Render(m.modal.View(m, m.w-8))
		block := lipgloss.Place(m.w, listH+2, lipgloss.Center, lipgloss.Center, content)
		return strings.Split(block, "\n")
	}
	if m.menu.open {
		m.overlayMenu(lines)
	}
	return lines
}

// treeLines renders the tree pane: breadcrumb, column header, listH rows.
func (m *Model) treeLines(width, listH int) []string {
	detW := m.detailWidth(width)
	lines := make([]string, 0, listH+2)
	lines = append(lines, m.renderBreadcrumb(width))
	lines = append(lines, m.renderHeader(width, detW))
	for i := 0; i < listH; i++ {
		lines = append(lines, m.renderRow(m.offset+i, width, detW))
	}
	return lines
}

// detailWidth sizes the Details column to its content, so connection
// targets render untruncated whenever the pane can fit them (§3.1). The
// name column keeps a minimum share as the backstop.
func (m *Model) detailWidth(listW int) int {
	w := 10
	for _, ni := range m.view {
		if dw := lipgloss.Width(m.nodeDetail(m.nodes[ni])); dw > w {
			w = dw
		}
	}
	if cap := listW - 30; w > cap {
		w = cap
	}
	if w < 10 {
		w = 10
	}
	return w
}

func (m *Model) renderBreadcrumb(width int) string {
	t := m.theme
	suffix := ""
	if m.anyOpening() {
		suffix = " " + spinnerFrames[m.tickN%len(spinnerFrames)]
	}
	avail := width - lipgloss.Width(suffix) - 2
	if avail < 8 {
		avail = 8
	}
	line := " " + t.Breadcrumb.Render(truncMiddle(sanitize(m.crumbs()), avail)) + suffix
	gap := width - lipgloss.Width(line)
	if gap > 0 {
		line += strings.Repeat(" ", gap)
	}
	return line
}

func (m *Model) anyOpening() bool {
	for _, st := range m.state {
		if st == connOpening {
			return true
		}
	}
	return false
}

func (m *Model) renderHeader(width, detW int) string {
	nameW := nameColWidth(width, detW)
	line := "  " + pad("Name", nameW) + " " + padLeft("Details", detW) + "  "
	return m.theme.Header.Render(pad(line, width))
}

func nameColWidth(listW, detW int) int {
	// Row = " " + icon(2) + name + " " + detail(detW) + " " + glyph(1) =
	// name + detW + 6; reserve 6 so the trailing glyph never truncates.
	w := listW - detW - 6
	if w < 8 {
		w = 8
	}
	return w
}

func (m *Model) renderRow(vi, width, detW int) string {
	t := m.theme
	if vi >= len(m.view) {
		if vi == 0 && len(m.view) == 0 {
			msg := "(no connections — press " + m.keys.KeyFor(ActNewConn) + " to add one)"
			if m.filterText != "" {
				msg = "(no matches)"
			}
			return t.Dim.Render(pad("  "+msg, width))
		}
		return strings.Repeat(" ", width)
	}
	ni := m.view[vi]
	if ni < 0 || ni >= len(m.nodes) {
		return strings.Repeat(" ", width)
	}
	n := m.nodes[ni]

	icon := "  "
	if n.HasChildren {
		if m.expanded[n.Path] {
			icon = "▾ "
		} else {
			icon = "▸ "
		}
	}
	indent := strings.Repeat("  ", n.Depth)
	icon = indent + icon
	nameW := nameColWidth(width, detW) - 2*n.Depth
	if nameW < 4 {
		nameW = 4
	}
	name := pad(truncMiddle(sanitize(m.displayName(n)), nameW), nameW)

	detail := truncEnd(m.nodeDetail(n), detW)
	glyph, gstyle := m.connGlyph(n)

	plain := " " + icon + name + " " + padLeft(detail, detW) + " " + glyph
	plain = pad(plain, width)

	if vi == m.cursor {
		return t.Cursor.Render(plain)
	}

	style := t.nodeStyle(n)
	line := " " + style.Render(icon+name) + " " + t.Dim.Render(padLeft(detail, detW)) +
		" " + gstyle.Render(glyph)
	// Narrow panes can undershoot the fixed column budget: clamp hard.
	line = ansi.Truncate(line, width, "")
	if gap := width - lipgloss.Width(line); gap > 0 {
		line += strings.Repeat(" ", gap)
	}
	return line
}

// displayName decorates structural nodes (group counts).
func (m *Model) displayName(n dbx.Node) string {
	if n.Kind == dbx.KGroup && n.Meta.Count >= 0 {
		return fmt.Sprintf("%s (%d)", n.Name, n.Meta.Count)
	}
	return n.Name
}

// nodeDetail is the right-hand column, switching on node kind.
func (m *Model) nodeDetail(n dbx.Node) string {
	switch n.Kind {
	case dbx.KSection:
		if kids := m.childCache[n.Path]; kids != nil {
			return fmt.Sprintf("%d", len(kids))
		}
		return ""
	case dbx.KConnection:
		return n.Meta.Engine + " · " + abbreviateHome(n.Meta.Target, m.home)
	case dbx.KDatabase:
		det := n.Meta.Owner
		if n.Meta.SizeBytes >= 0 {
			det += " · " + shortBytes(n.Meta.SizeBytes)
		}
		return strings.TrimPrefix(det, " · ")
	case dbx.KSchema:
		return n.Meta.Owner
	case dbx.KTable:
		det := fmtRows(n.Meta.RowsEst)
		if n.Meta.SizeBytes >= 0 {
			if det != "" {
				det += " · "
			}
			det += shortBytes(n.Meta.SizeBytes)
		}
		return det
	case dbx.KView:
		return "view"
	case dbx.KRole:
		det := "nologin"
		if n.Meta.CanLogin {
			det = "login"
		}
		if n.Meta.Super {
			det += " · super"
		}
		return det
	case dbx.KColumn:
		det := n.Meta.TypeName
		if n.Meta.PK {
			det += " · PK"
		} else if n.Meta.NotNull {
			det += " · NN"
		}
		return strings.TrimPrefix(det, " · ")
	case dbx.KIndex:
		det := n.Meta.Columns
		if n.Meta.Unique {
			det = "unique · " + det
		}
		return det
	case dbx.KGroup:
		if n.Group == dbx.GroupRoles && n.Meta.Target != "" {
			return n.Meta.Target // the server this roles entry belongs to
		}
	}
	return ""
}

// connGlyph is the status glyph column (connections only).
func (m *Model) connGlyph(n dbx.Node) (string, lipgloss.Style) {
	t := m.theme
	if n.Kind != dbx.KConnection {
		return " ", t.Dim
	}
	switch m.state[n.ConnID] {
	case connOpen:
		if c, ok := m.connByID[n.ConnID]; ok && c.ReadOnly() {
			return "⏸", t.ConnRow // read-only session (blue, per the glyph table)
		}
		return "●", t.GlyphOpen
	case connOpening:
		return "◐", t.GlyphBusy
	case connFailed:
		return "✗", t.GlyphFailed
	}
	return "○", t.GlyphClosed
}

// fmtRows renders a row estimate: exact under the cap, "1M+" above it.
func fmtRows(n int64) string {
	switch {
	case n < 0:
		return ""
	case n > 1_000_000:
		return "1M+ rows"
	case n == 1:
		return "1 row"
	default:
		return fmt.Sprintf("%d rows", n)
	}
}

// --- info panel -----------------------------------------------------------

// infoPanel renders the passive right panel describing the cursor node
// from already-loaded state — it never triggers I/O.
func (m *Model) infoPanel(width, height int) []string {
	t := m.theme
	inner := width - 2
	if inner < 4 {
		inner = 4
	}
	n := m.currentNode()
	var content []string
	add := func(s string) { content = append(content, " "+truncEnd(s, inner-1)) }
	addKV := func(k, v string) { add(t.PanelMeta.Render(pad(k, 12)) + sanitize(v)) }

	if n == nil {
		add(t.PanelMeta.Render("nothing selected"))
	} else {
		content = append(content, " "+t.PanelTitle.Render(truncMiddle(sanitize(m.displayName(*n)), inner-1)))
		add("")
		switch n.Kind {
		case dbx.KSection:
			addKV("section", n.Name)
			if n.Meta.Locality == locRoles {
				addKV("servers", fmt.Sprintf("%d", len(m.childCache[n.Path])))
				add("")
				add(t.PanelMeta.Render("cluster roles, grouped per connected server —"))
				add(t.PanelMeta.Render("roles belong to a server, not a connection string."))
			} else if isAnnex(n.Meta.Locality) {
				addKV("databases", fmt.Sprintf("%d", len(m.childCache[n.Path])))
				add("")
				add(t.PanelMeta.Render("databases discovered on connected servers,"))
				add(t.PanelMeta.Render("browsed with the source connection's credentials."))
				add(t.PanelMeta.Render("Save one as its own connection with " + m.keys.KeyFor(ActNewConn) + "."))
			} else {
				addKV("connections", fmt.Sprintf("%d", len(m.childCache[n.Path])))
				add("")
				add(t.PanelMeta.Render(m.keys.KeyFor(ActNewConn) + " adds a connection"))
			}
		case dbx.KConnection:
			c, ok := m.connByID[n.ConnID]
			if !ok {
				break
			}
			addKV("engine", c.Engine)
			addKV("section", c.Locality)
			if c.Engine == "sqlite" {
				addKV("file", abbreviateHome(c.Path, m.home))
			} else {
				addKV("host", fmt.Sprintf("%s:%d", c.Host, c.Port))
				addKV("database", c.DBName)
				addKV("user", c.Username)
			}
			if c.Secret != "" {
				if m.revealID == c.ID {
					add(t.PanelMeta.Render(pad("password", 12)) + t.Danger.Render(sanitize(c.Secret)))
				} else {
					addKV("password", "••••••••  ("+m.keys.KeyFor(ActRevealSecret)+" reveals)")
				}
			}
			addKV("state", m.stateLabel(n.ConnID))
			if e := m.connErr[n.ConnID]; e != "" {
				add("")
				add(t.Danger.Render(truncEnd(e, inner-2)))
			}
			if info := m.serverInfo[n.ConnID]; len(info) > 0 {
				add("")
				for _, kv := range info {
					addKV(kv.Key, kv.Value)
				}
			}
			if c.LastUsedAt != "" {
				add("")
				addKV("last used", c.LastUsedAt)
			}
		case dbx.KGroup:
			if n.Meta.Count >= 0 {
				addKV("items", fmt.Sprintf("%d", n.Meta.Count))
			} else {
				add(t.PanelMeta.Render("expand to load"))
			}
		case dbx.KTable, dbx.KView:
			kind := "table"
			if n.Kind == dbx.KView {
				kind = "view"
			}
			addKV("kind", kind)
			if r := fmtRows(n.Meta.RowsEst); r != "" {
				addKV("rows", r)
			}
			// Columns, when already fetched for the subtree.
			if cols, ok := m.childCache[n.ChildPath("g:cols")]; ok {
				add("")
				add(t.PanelMeta.Render("columns"))
				for _, c := range cols {
					flag := ""
					if c.Meta.PK {
						flag = "  PK"
					} else if c.Meta.NotNull {
						flag = "  NN"
					}
					add("  " + pad(sanitize(c.Name), 20) + t.PanelMeta.Render(c.Meta.TypeName+flag))
				}
			} else {
				add("")
				add(t.PanelMeta.Render("expand Columns to inspect"))
			}
		case dbx.KDatabase:
			addKV("database", n.Name)
			addKV("owner", n.Meta.Owner)
			if n.Meta.SizeBytes >= 0 {
				addKV("size", humanBytes(n.Meta.SizeBytes))
			}
		case dbx.KSchema:
			addKV("schema", n.Name)
			addKV("database", n.Ref.Database)
			addKV("owner", n.Meta.Owner)
		case dbx.KRole:
			addKV("role", n.Name)
			addKV("can log in", yesNo(n.Meta.CanLogin))
			addKV("superuser", yesNo(n.Meta.Super))
			if n.Meta.MemberOf != "" {
				addKV("member of", n.Meta.MemberOf)
			}
		case dbx.KColumn:
			addKV("column of", n.Ref.Name)
			addKV("type", n.Meta.TypeName)
			addKV("primary key", yesNo(n.Meta.PK))
			addKV("not null", yesNo(n.Meta.NotNull))
		case dbx.KIndex:
			addKV("index of", n.Ref.Name)
			addKV("unique", yesNo(n.Meta.Unique))
			addKV("columns", n.Meta.Columns)
		}
	}

	out := make([]string, height)
	for i := range out {
		line := ""
		if i < len(content) {
			line = content[i]
		}
		if gap := width - lipgloss.Width(line); gap > 0 {
			line += strings.Repeat(" ", gap)
		}
		out[i] = line
	}
	return out
}

// activeConnName is the currently connected database's name ("" when
// nothing is connected — there is at most one, per §3.2).
func (m *Model) activeConnName() string {
	for _, c := range m.conns {
		if m.state[c.ID] == connOpen {
			return c.Name
		}
	}
	return ""
}

func (m *Model) stateLabel(id int64) string {
	switch m.state[id] {
	case connOpen:
		return "connected"
	case connOpening:
		return "connecting…"
	case connFailed:
		return "connection failed"
	}
	return "not connected"
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// --- bottom bars ------------------------------------------------------------

func (m *Model) renderFilterLine() string {
	t := m.theme
	var line string
	if m.filtering {
		line = " " + m.filterInput.View()
	} else {
		line = " filter: " + m.filterText + t.Dim.Render("  (esc clears)")
	}
	gap := m.w - lipgloss.Width(line)
	if gap > 0 {
		line += strings.Repeat(" ", gap)
	}
	return line
}

func (m *Model) renderStatusBar() string {
	t := m.theme
	var left string
	if m.status.text != "" {
		style := t.StatusInfo
		switch m.status.level {
		case levelOK:
			style = t.StatusOK
		case levelWarn:
			style = t.StatusWarn
		case levelErr:
			style = t.StatusErr
		}
		left = style.Render(" " + truncEnd(m.status.text, m.w-2) + " ")
	} else {
		openN := 0
		for _, st := range m.state {
			if st == connOpen {
				openN++
			}
		}
		info := fmt.Sprintf(" %d connection(s) · %d connected", len(m.conns), openN)
		left = t.StatusInfo.Render(info)
	}

	// The connection indicator is always visible: green ● + name while
	// connected, red ● while not (§3.1).
	ind := t.StatusErr.Render("● disconnected ")
	if name := m.activeConnName(); name != "" {
		ind = t.StatusOK.Render("● " + truncEnd(sanitize(name), 24) + " ")
	}
	rightR := ind + t.StatusBar.Render("· ? help ")

	gap := m.w - lipgloss.Width(left) - lipgloss.Width(rightR)
	if gap < 1 {
		rightR = ""
		gap = m.w - lipgloss.Width(left)
		if gap < 0 {
			gap = 0
		}
	}
	return left + t.StatusBar.Render(strings.Repeat(" ", gap)) + rightR
}
