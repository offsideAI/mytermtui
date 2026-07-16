package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The nano-style shortcut bar: a box-enclosed, two-row cheat sheet of the
// most useful keys, pinned above the status bar. Rows adapt to what is
// focused (browse tree, menus, a modal, the filter input) and always show
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
	case m.focusRight:
		if m.tab == tabSQL {
			s := m.currentSQLSession(false)
			if s != nil && s.editorFocused {
				return [][]hint{{
					{key: "i", label: "insert"},
					{key: "esc", label: "normal"},
					{key: "^r/:w", label: "run"},
					{key: "tab", label: "results"},
					{key: ":q", label: "tree"},
				}}
			}
			return [][]hint{
				{
					{key: "↑↓←→", label: "cell"},
					{key: "enter", label: "value"},
					{key: "e/E", label: "explain"},
					{key: "esc", label: "cancel/editor"},
				},
				{
					{key: "i", label: "editor"},
					{key: "y/Y", label: "copy"},
					{key: "[/]", label: "tabs"},
					{key: "tab", label: "tree"},
				},
			}
		}
		if m.tab == tabData {
			return [][]hint{
				{
					{key: "↑↓←→", label: "cell"},
					{key: "J/K", label: "page"},
					{key: "enter", label: "value"},
					{key: "y/Y", label: "copy"},
				},
				{
					{key: "[/]", label: "tabs"},
					{key: "g/G", label: "top/bottom"},
					{key: "tab", label: "tree"},
					{act: ActQuit, label: "quit"},
				},
			}
		}
		return [][]hint{{
			{key: "[/]", label: "tabs"},
			{key: "tab", label: "tree"},
			{act: ActHelp, label: "help"},
			{act: ActQuit, label: "quit"},
		}}
	}
	return [][]hint{
		{
			{act: ActMenu, label: "menu"},
			{act: ActHelp, label: "help"},
			{key: "enter", label: "expand"},
			{act: ActConnect, label: "connect"},
			{act: ActDisconnect, label: "disconnect"},
			{act: ActFilter, label: "filter"},
		},
		{
			{act: ActNewConn, label: "new conn"},
			{act: ActEditConn, label: "edit"},
			{act: ActDeleteConn, label: "delete"},
			{act: ActSwapPane, label: "panel"},
			{act: ActRefresh, label: "refresh"},
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
	keyStyle := t.ModalTitle  // accent, bold — pops like nano's inverse keys
	labelStyle := t.PanelMeta // dim
	borderStyle := t.PanelMeta

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
