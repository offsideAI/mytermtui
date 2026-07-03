package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/offsideai/mytermtui/internal/fsx"
	"github.com/offsideai/mytermtui/internal/icloud"
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
	if m.downloadBarVisible() {
		out = append(out, m.renderDownloadBar())
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
	title := " mytermtui "
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

// --- main block: breadcrumb + header + rows (+ preview, menu, modal) ----------

func (m *Model) renderMain() []string {
	listH := m.listHeight()
	pw := 0
	if m.previewOn && m.w >= 70 {
		pw = min(44, m.w/3)
	}
	listW := m.w - pw

	lines := make([]string, 0, listH+2)
	lines = append(lines, m.renderBreadcrumb(listW))
	lines = append(lines, m.renderHeader(listW))
	for i := 0; i < listH; i++ {
		lines = append(lines, m.renderRow(m.offset+i, listW))
	}

	if pw > 0 {
		prev := m.renderPreview(pw, listH+2)
		for i := range lines {
			lines[i] += prev[i]
		}
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

func (m *Model) renderBreadcrumb(width int) string {
	t := m.theme
	path := abbreviateHome(m.cwd, m.home)
	prefix := " "
	if icloud.InICloud(m.cwd) {
		prefix = " ☁ "
	}
	suffix := ""
	if m.loading {
		suffix = " " + spinnerFrames[m.tickN%len(spinnerFrames)]
	}
	if m.anchor >= 0 {
		suffix += " [range-select]"
	}
	avail := width - lipgloss.Width(prefix) - lipgloss.Width(suffix) - 1
	if avail < 8 {
		avail = 8
	}
	line := prefix + t.Breadcrumb.Render(truncMiddle(sanitize(path), avail)) + suffix
	gap := width - lipgloss.Width(line)
	if gap > 0 {
		line += strings.Repeat(" ", gap)
	}
	return line
}

func (m *Model) renderHeader(width int) string {
	nameW := nameColWidth(width)
	line := "    " + pad("Name", nameW) + " " + padLeft("Size", 9) + " " + padLeft("Modified", 12) + "  ☁"
	return m.theme.Header.Render(pad(line, width))
}

func nameColWidth(listW int) int {
	w := listW - 31
	if w < 8 {
		w = 8
	}
	return w
}

func (m *Model) renderRow(vi, width int) string {
	t := m.theme
	if vi >= len(m.view) {
		if vi == 0 && len(m.view) == 0 {
			msg := "(empty)"
			if m.filterText != "" {
				msg = "(no matches)"
			}
			return t.HiddenDim.Render(pad("    "+msg, width))
		}
		return strings.Repeat(" ", width)
	}
	e := m.entries[m.view[vi]]

	sel := " "
	if m.selected[e.Path] || m.inRange(vi) {
		sel = "●"
	}
	icon := "  "
	if e.IsDir {
		icon = "▸ "
	} else if e.IsLink {
		icon = "@ "
	}
	nameW := nameColWidth(width)
	name := pad(truncMiddle(sanitize(e.Name), nameW), nameW)

	size := "--"
	if !e.IsDir {
		size = shortBytes(e.Size)
	}
	date := humanTime(e.ModTime, time.Now())
	glyph, gstyle := m.glyphFor(e)

	plain := sel + " " + icon + name + " " + padLeft(size, 9) + " " + padLeft(date, 12) + "  " + glyph
	plain = pad(plain, width)

	switch {
	case vi == m.cursor:
		return t.Cursor.Render(plain)
	case sel == "●":
		return t.Selected.Render(plain)
	}

	style := t.classStyle(e.Class())
	if e.Hidden {
		style = t.HiddenDim
	}
	line := " " + " " + style.Render(icon+name) + " " + padLeft(size, 9) + " " +
		t.HiddenDim.Render(padLeft(date, 12)) + "  " + gstyle.Render(glyph)
	gap := width - lipgloss.Width(line)
	if gap > 0 {
		line += strings.Repeat(" ", gap)
	}
	return line
}

// glyphFor picks the iCloud status glyph and style for an entry. Queue
// state comes from the path-keyed index (setQsnap), so this stays O(1)
// per row even with a 200k-item download queue.
func (m *Model) glyphFor(e fsx.Entry) (string, lipgloss.Style) {
	t := m.theme
	if !e.InICloud {
		return " ", t.HiddenDim
	}
	// Queue states take precedence so the user sees ◌ / ⇣ live. The
	// comma-ok form matters: the zero ItemState is StateQueued.
	if st, ok := m.qstate[e.Path]; ok {
		switch st {
		case icloud.StateQueued:
			return "◌", t.GlyphQueued
		case icloud.StateStarting, icloud.StateDownloading, icloud.StateStalled:
			return "⇣", t.GlyphActive
		}
	}
	if e.Dataless {
		return "☁", t.GlyphCloud
	}
	if e.IsDir {
		// No recursive scan here: a materialized dir's listing is local,
		// but children may still be evicted. Neutral dot.
		return "·", t.HiddenDim
	}
	return "✓", t.GlyphLocal
}

// --- preview panel -------------------------------------------------------------

func (m *Model) renderPreview(pw, height int) []string {
	t := m.theme
	inner := pw - 3 // "│ " border + trailing space
	if inner < 4 {
		inner = 4
	}
	var content []string
	e := m.currentEntry()
	if e == nil {
		content = []string{t.PreviewMeta.Render("nothing selected")}
	} else {
		content = append(content,
			t.PreviewTitle.Render(truncMiddle(sanitize(e.Name), inner)),
			t.PreviewMeta.Render(truncEnd(e.Kind()+" · "+humanBytes(e.Size), inner)),
			"",
		)
		for _, l := range m.prevLines {
			content = append(content, truncEnd(l, inner))
		}
	}
	out := make([]string, height)
	border := t.PreviewMeta.Render("│")
	for i := 0; i < height; i++ {
		line := ""
		if i < len(content) {
			line = content[i]
		}
		gap := inner - lipgloss.Width(line)
		if gap > 0 {
			line += strings.Repeat(" ", gap)
		}
		out[i] = border + " " + line + " "
	}
	return out
}

// --- bottom bars ------------------------------------------------------------------

func (m *Model) renderFilterLine() string {
	t := m.theme
	var line string
	if m.filtering {
		line = " " + m.filterInput.View()
	} else {
		line = " filter: " + m.filterText + t.HiddenDim.Render("  (esc clears)")
	}
	gap := m.w - lipgloss.Width(line)
	if gap > 0 {
		line += strings.Repeat(" ", gap)
	}
	return line
}

func (m *Model) renderDownloadBar() string {
	t := m.theme
	s := m.qsnap
	frac := 0.0
	if s.TotalBytes > 0 {
		frac = float64(s.GotBytes) / float64(s.TotalBytes)
	}
	cur := ""
	if s.Current != "" {
		cur = fmt.Sprintf(" %s %3.0f%%", truncMiddle(sanitize(filepath.Base(s.Current)), 28), 100*s.CurrentPct)
	}
	label := fmt.Sprintf(" ⇣ %d/%d · %s of %s", s.DoneN, s.DoneN+s.ActiveN, humanBytes(s.GotBytes), humanBytes(s.TotalBytes))
	if s.Paused {
		label += " · paused"
	}
	barW := m.w - lipgloss.Width(label) - lipgloss.Width(cur) - 3
	bar := ""
	if barW >= 10 {
		bar = " " + t.BarFill.Render(progressBar(frac, barW))
	}
	line := t.DownloadBar.Render(label) + bar + cur
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
	} else if m.op != nil {
		text := " " + spinnerFrames[m.tickN%len(spinnerFrames)] + " " + m.op.desc
		if p := m.op.prog; p != nil && p.Total() > 0 {
			text += fmt.Sprintf(" %s / %s", humanBytes(p.Done()), humanBytes(p.Total()))
			if l := p.Label(); l != "" {
				text += " · " + truncMiddle(sanitize(l), 24)
			}
		}
		left = t.StatusWarn.Render(text + " ")
	} else {
		info := fmt.Sprintf(" %d items", len(m.view))
		if n := len(m.selected); n > 0 {
			var bytes int64
			for _, e := range m.entries {
				if m.selected[e.Path] {
					bytes += e.Size
				}
			}
			info = fmt.Sprintf(" %d selected (%s) · %d items", n, humanBytes(bytes), len(m.view))
		}
		left = t.StatusInfo.Render(info)
	}

	dir := "↑"
	if !m.sortAsc {
		dir = "↓"
	}
	right := fmt.Sprintf("sort %s %s ", m.sortBy, dir)
	if m.showHidden {
		right = "hidden on · " + right
	}
	if m.clip.paths != nil {
		verb := "copied"
		if m.clip.cut {
			verb = "cut"
		}
		right = fmt.Sprintf("%d %s · ", len(m.clip.paths), verb) + right
	}
	right += "? help "
	rightR := t.StatusBar.Render(right)

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
