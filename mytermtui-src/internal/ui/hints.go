package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The nano-style shortcut bar: a box-enclosed, two-row cheat sheet of the
// most useful keys, pinned above the status bar. Rows adapt to what is
// focused (browse list, menus, a modal, the filter input) and always show
// the *live* bindings, so config remaps stay truthful.

type hint struct {
	act   Action // resolved through the keymap ("" → use key literally)
	key   string // literal key text when act == ""
	label string
}

func (m *Model) hintRows() [][]hint {
	switch {
	case m.modal != nil:
		return [][]hint{{
			{key: "↑/↓", label: "move"},
			{key: "enter", label: "confirm"},
			{key: "esc", label: "cancel"},
		}}
	case m.menu.open:
		return [][]hint{{
			{key: "←/→", label: "menus"},
			{key: "↑/↓", label: "items"},
			{key: "enter", label: "run"},
			{key: "esc", label: "close"},
		}}
	case m.filtering:
		return [][]hint{{
			{key: "type", label: "filter"},
			{key: "enter", label: "keep"},
			{key: "esc", label: "clear"},
		}}
	}
	row1 := []hint{
		{act: ActMenu, label: "menu"},
		{act: ActHelp, label: "help"},
		{key: "enter", label: "open"},
		{key: "bksp", label: "up"},
	}
	if m.split() {
		row1 = append(row1, hint{act: ActSwapPane, label: "panel"})
	}
	return [][]hint{
		append(row1,
			hint{act: ActSelect, label: "select"},
			hint{act: ActDownload, label: "download"},
			hint{act: ActEvict, label: "evict"},
		),
		{
			{act: ActCopy, label: "copy"},
			{act: ActCut, label: "cut"},
			{act: ActPaste, label: "paste"},
			{act: ActRename, label: "rename"},
			{act: ActTrash, label: "trash"},
			{act: ActFilter, label: "filter"},
			{act: ActQueue, label: "queue"},
			{act: ActQuit, label: "quit"},
		},
	}
}

// prettyKey compacts a binding for display, nano-style.
func prettyKey(k string) string {
	switch k {
	case " ", "space":
		return "spc"
	case "backspace":
		return "bksp"
	case "delete":
		return "del"
	}
	if strings.HasPrefix(k, "ctrl+") {
		return "^" + strings.TrimPrefix(k, "ctrl+")
	}
	return k
}

func (m *Model) renderHintBar() []string {
	t := m.theme
	keyStyle := t.ModalTitle    // accent, bold — pops like nano's inverse keys
	labelStyle := t.PreviewMeta // dim
	borderStyle := t.PreviewMeta

	inner := m.w - 4 // "│ " + " │"
	if inner < 10 {
		inner = 10
	}

	rows := m.hintRows()
	lines := make([]string, 0, 4)
	lines = append(lines, borderStyle.Render("╭"+strings.Repeat("─", m.w-2)+"╮"))

	renderRow := func(cells []hint) string {
		n := len(cells)
		cellW := inner / n
		var b strings.Builder
		used := 0
		for i, c := range cells {
			key := c.key
			if c.act != "" {
				key = prettyKey(m.keys.KeyFor(c.act))
			}
			w := cellW
			if i == n-1 {
				w = inner - used // last cell absorbs the remainder
			}
			text := key + " " + c.label
			if tw := lipgloss.Width(text); tw > w {
				b.WriteString(pad(truncEnd(text, w), w)) // too tight to style
			} else {
				b.WriteString(keyStyle.Render(key) + " " + labelStyle.Render(c.label) +
					strings.Repeat(" ", w-tw))
			}
			used += w
		}
		return b.String()
	}

	for i := 0; i < 2; i++ {
		content := ""
		if i < len(rows) {
			content = renderRow(rows[i])
		}
		if gap := inner - lipgloss.Width(content); gap > 0 {
			content += strings.Repeat(" ", gap)
		}
		lines = append(lines, borderStyle.Render("│ ")+content+borderStyle.Render(" │"))
	}
	lines = append(lines, borderStyle.Render("╰"+strings.Repeat("─", m.w-2)+"╯"))
	return lines
}
