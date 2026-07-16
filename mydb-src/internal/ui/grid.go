package ui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/offsideai/mydb/internal/dbx"
)

// gridView is the shared read-only results view: one result set plus a
// cell cursor and horizontal scroll. The Data tab and the SQL tab's
// results pane both embed it.
type gridView struct {
	res    *dbx.Result
	row    int // cursor row within the loaded rows
	col    int // cursor column
	colOff int // first visible column
}

// setResult installs a fresh result set, clamping the cursor.
func (gv *gridView) setResult(res *dbx.Result) {
	gv.res = res
	if res == nil {
		gv.row, gv.col, gv.colOff = 0, 0, 0
		return
	}
	if gv.row >= len(res.Rows) {
		gv.row = len(res.Rows) - 1
	}
	if gv.row < 0 {
		gv.row = 0
	}
	if gv.col >= len(res.Columns) {
		gv.col = 0
		gv.colOff = 0
	}
}

// gridState is the Data tab: a gridView bound to one table/view.
type gridState struct {
	gridView
	path    string // tree node the grid is bound to
	ref     dbx.ObjectRef
	connID  int64
	title   string
	rowsEst int64

	page    int // 0-based
	loading bool
	err     string
}

type pageLoadedMsg struct {
	path string
	page int
	res  *dbx.Result
	err  error
}

type copyDoneMsg struct {
	what string
	err  error
}

func pageCmd(conn dbx.Conn, path string, ref dbx.ObjectRef, page, size int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), ioTimeout)
		defer cancel()
		res, err := conn.ReadPage(ctx, ref, dbx.PageReq{Offset: page * size, Limit: size})
		return pageLoadedMsg{path: path, page: page, res: res, err: err}
	}
}

func copyCmd(what, text string) tea.Cmd {
	return func() tea.Msg {
		bin, err := exec.LookPath("pbcopy")
		if err != nil {
			return copyDoneMsg{what: what, err: fmt.Errorf("no clipboard tool (pbcopy)")}
		}
		cmd := exec.Command(bin)
		cmd.Stdin = strings.NewReader(text)
		return copyDoneMsg{what: what, err: cmd.Run()}
	}
}

// maybeLoadGrid binds the grid to the cursor node when it is a table or
// view, loading page 0 if the binding changed.
func (m *Model) maybeLoadGrid() tea.Cmd {
	n := m.currentNode()
	if n == nil || (n.Kind != dbx.KTable && n.Kind != dbx.KView) {
		return nil // keep whatever the grid last showed
	}
	if m.grid.path == n.Path && (m.grid.res != nil || m.grid.loading) {
		return nil
	}
	conn := m.open[n.ConnID]
	if conn == nil {
		m.grid = gridState{path: n.Path, err: "not connected"}
		return nil
	}
	m.grid = gridState{
		path: n.Path, ref: n.Ref, connID: n.ConnID,
		title: n.Name, rowsEst: n.Meta.RowsEst, loading: true,
	}
	return pageCmd(conn, n.Path, n.Ref, 0, m.cfg.Query.PageSize)
}

// gridNavKey handles the navigation/copy/popup keys shared by every
// gridView. The second return reports whether the key was consumed.
func (m *Model) gridNavKey(gv *gridView, title, key string) (tea.Cmd, bool) {
	if gv.res == nil {
		return nil, false
	}
	rows, cols := gv.res.Rows, gv.res.Columns
	switch key {
	case "down", "j":
		if gv.row < len(rows)-1 {
			gv.row++
		}
	case "up", "k":
		if gv.row > 0 {
			gv.row--
		}
	case "g":
		gv.row = 0
	case "G":
		if len(rows) > 0 {
			gv.row = len(rows) - 1
		}
	case "left", "h":
		if gv.col > 0 {
			gv.col--
		}
		if gv.col < gv.colOff {
			gv.colOff = gv.col
		}
	case "right", "l":
		if gv.col < len(cols)-1 {
			gv.col++
		}
	case "enter":
		if v, colName, ok := gridCell(gv); ok {
			m.modal = &InfoModal{Title: title + "." + colName, Lines: cellLines(v)}
		}
	case "y":
		if v, colName, ok := gridCell(gv); ok {
			return copyCmd("cell "+colName, v.S), true
		}
	case "Y":
		if gv.row < len(rows) {
			parts := make([]string, len(rows[gv.row]))
			for i, v := range rows[gv.row] {
				parts[i] = v.S
			}
			return copyCmd("row", strings.Join(parts, "\t")), true
		}
	default:
		return nil, false
	}
	return nil, true
}

// gridKey handles keys for the Data tab: shared navigation plus paging.
func (m *Model) gridKey(key string) tea.Cmd {
	g := &m.grid
	if cmd, handled := m.gridNavKey(&g.gridView, g.title, key); handled {
		return cmd
	}
	if g.res == nil {
		return nil
	}
	switch key {
	case "J", "pgdown":
		if g.res.Truncated {
			return m.loadGridPage(g.page + 1)
		}
	case "K", "pgup":
		if g.page > 0 {
			return m.loadGridPage(g.page - 1)
		}
	}
	return nil
}

func (m *Model) loadGridPage(page int) tea.Cmd {
	g := &m.grid
	conn := m.open[g.connID]
	if conn == nil {
		g.err = "not connected"
		return nil
	}
	g.loading = true
	return pageCmd(conn, g.path, g.ref, page, m.cfg.Query.PageSize)
}

func gridCell(gv *gridView) (dbx.Value, string, bool) {
	if gv.res == nil || gv.row >= len(gv.res.Rows) || gv.col >= len(gv.res.Columns) {
		return dbx.Value{}, "", false
	}
	return gv.res.Rows[gv.row][gv.col], gv.res.Columns[gv.col].Name, true
}

// cellLines wraps a full cell value for the popup.
func cellLines(v dbx.Value) []string {
	if v.Kind == dbx.VNull {
		return []string{"NULL"}
	}
	var out []string
	for _, raw := range strings.Split(v.S, "\n") {
		raw = sanitize(raw)
		for len(raw) > 76 {
			out = append(out, raw[:76])
			raw = raw[76:]
		}
		out = append(out, raw)
	}
	return out
}

// --- rendering -------------------------------------------------------------

const (
	gridColMin = 4
	gridColMax = 28
)

// renderGrid draws the Data tab: pinned header, rows, footer.
func (m *Model) renderGrid(width, height int, focused bool) []string {
	t := m.theme
	g := &m.grid
	out := make([]string, 0, height)
	blank := func(s string) string {
		if gap := width - lipgloss.Width(s); gap > 0 {
			s += strings.Repeat(" ", gap)
		}
		return s
	}

	n := m.currentNode()
	boundToCursor := n != nil && (n.Kind == dbx.KTable || n.Kind == dbx.KView)
	switch {
	case g.res == nil && g.err != "":
		out = append(out, blank(" "+t.Danger.Render(truncEnd(g.err, width-2))))
	case g.res == nil && g.loading:
		out = append(out, blank(" "+t.Dim.Render("loading…")))
	case g.res == nil && !boundToCursor:
		out = append(out, blank(" "+t.Dim.Render("select a table or view")))
	case g.res == nil:
		out = append(out, blank(" "+t.Dim.Render("loading…")))
	default:
		out = m.gridRows(&g.gridView, width, height-1, focused)
		out = append(out, blank(m.gridFooter(width)))
	}
	for len(out) < height {
		out = append(out, strings.Repeat(" ", width))
	}
	return out[:height]
}

// gridWidths sizes the visible columns from colOff.
func gridWidths(gv *gridView) []int {
	cols := gv.res.Columns
	widths := make([]int, len(cols))
	for ci := range cols {
		w := lipgloss.Width(cols[ci].Name)
		for _, row := range gv.res.Rows {
			if cw := lipgloss.Width(sanitize(row[ci].S)); cw > w {
				w = cw
			}
		}
		if w < gridColMin {
			w = gridColMin
		}
		if w > gridColMax {
			w = gridColMax
		}
		widths[ci] = w
	}
	return widths
}

func (m *Model) gridRows(gv *gridView, width, bodyH int, focused bool) []string {
	t := m.theme
	widths := gridWidths(gv)

	// Keep the cursor column visible: advance colOff until it fits.
	if gv.col < gv.colOff {
		gv.colOff = gv.col
	}
	for {
		used := 1
		fits := false
		for ci := gv.colOff; ci < len(widths); ci++ {
			used += widths[ci] + 2
			if ci == gv.col {
				fits = used <= width
				break
			}
		}
		if fits || gv.colOff >= gv.col {
			break
		}
		gv.colOff++
	}

	renderLine := func(cells []string, styles []lipgloss.Style) string {
		var b strings.Builder
		b.WriteString(" ")
		used := 1
		for i := gv.colOff; i < len(widths) && used < width; i++ {
			w := widths[i]
			if used+w > width {
				w = width - used
			}
			if w < 1 {
				break
			}
			b.WriteString(styles[i].Render(pad(truncEnd(cells[i], w), w)))
			used += w
			if used+2 <= width {
				b.WriteString("  ")
				used += 2
			}
		}
		s := b.String()
		if gap := width - lipgloss.Width(s); gap > 0 {
			s += strings.Repeat(" ", gap)
		}
		return s
	}

	cols := gv.res.Columns
	headCells := make([]string, len(cols))
	headStyles := make([]lipgloss.Style, len(cols))
	for i, c := range cols {
		headCells[i] = c.Name
		headStyles[i] = t.Header
		if i == gv.col && focused {
			headStyles[i] = t.Header.Bold(true)
		}
	}
	out := []string{renderLine(headCells, headStyles)}

	// Keep the cursor row on screen.
	rowOff := 0
	if gv.row >= bodyH-1 {
		rowOff = gv.row - (bodyH - 2)
	}
	for ri := rowOff; ri < len(gv.res.Rows) && len(out) < bodyH; ri++ {
		row := gv.res.Rows[ri]
		cells := make([]string, len(row))
		styles := make([]lipgloss.Style, len(row))
		for i, v := range row {
			cells[i] = sanitize(v.S)
			switch v.Kind {
			case dbx.VNull:
				cells[i] = "␀"
				styles[i] = t.Dim
			case dbx.VNumber:
				styles[i] = t.Table
			case dbx.VBytes:
				styles[i] = t.Role
			default:
				styles[i] = t.Row
			}
			if focused && ri == gv.row && i == gv.col {
				styles[i] = t.Cursor
			}
		}
		line := renderLine(cells, styles)
		if focused && ri == gv.row {
			// Whole-row hint under the cell cursor.
			line = strings.Replace(line, " ", ">", 1)
		}
		out = append(out, line)
	}
	return out
}

func (m *Model) gridFooter(width int) string {
	t := m.theme
	g := &m.grid
	first := g.page*m.cfg.Query.PageSize + 1
	last := g.page*m.cfg.Query.PageSize + len(g.res.Rows)
	if len(g.res.Rows) == 0 {
		first = 0
	}
	total := ""
	if g.rowsEst >= 0 {
		total = fmt.Sprintf(" of ~%d", g.rowsEst)
	}
	s := fmt.Sprintf(" rows %d–%d%s · %.1f ms", first, last, total,
		float64(g.res.Elapsed.Microseconds())/1000)
	if g.res.Truncated {
		s += " · J next page"
	}
	if g.page > 0 {
		s += " · K prev"
	}
	if g.loading {
		s += " · " + spinnerFrames[m.tickN%len(spinnerFrames)]
	}
	if g.err != "" {
		s += " · " + g.err
	}
	return t.Dim.Render(truncEnd(s, width))
}
