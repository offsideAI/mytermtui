package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The workspace is the right panel: a tab strip over per-tab content.
// `tab` focuses it, `[`/`]` switch tabs, `tab`/`esc` return to the tree.

const (
	tabInfo = iota
	tabData
	tabCount
)

var tabNames = [tabCount]string{"Info", "Data"}

// swapFocus toggles which side receives keys.
func (m *Model) swapFocus() tea.Cmd {
	if !m.panelOn {
		m.panelOn = true
	}
	m.focusRight = !m.focusRight
	if m.focusRight && m.tab == tabData {
		return m.maybeLoadGrid()
	}
	return nil
}

// switchTab activates a workspace tab (with wraparound).
func (m *Model) switchTab(delta int) tea.Cmd {
	m.tab = (m.tab + delta + tabCount) % tabCount
	if m.tab == tabData {
		return m.maybeLoadGrid()
	}
	return nil
}

// workspaceKey handles keys while the workspace is focused.
func (m *Model) workspaceKey(key string) tea.Cmd {
	switch key {
	case "tab", "esc":
		m.focusRight = false
		return nil
	case "[":
		return m.switchTab(-1)
	case "]":
		return m.switchTab(+1)
	case "ctrl+q":
		return tea.Quit
	case "?", "f1":
		m.modal = &HelpModal{}
		return nil
	}
	if m.tab == tabData {
		return m.gridKey(key)
	}
	return nil
}

// renderWorkspace draws the tab strip plus the active tab's content.
func (m *Model) renderWorkspace(width, height int) []string {
	lines := []string{m.renderTabBar(width)}
	body := height - 1
	if body < 1 {
		body = 1
	}
	switch m.tab {
	case tabData:
		lines = append(lines, m.renderGrid(width, body, m.focusRight)...)
	default:
		lines = append(lines, m.infoPanel(width, body)...)
	}
	return lines[:height]
}

func (m *Model) renderTabBar(width int) string {
	t := m.theme
	var b strings.Builder
	b.WriteString(" ")
	for i, name := range tabNames {
		style := t.Dim
		if i == m.tab {
			style = t.PanelTitle
			if m.focusRight {
				style = style.Underline(true)
			}
		}
		b.WriteString(style.Render(name))
		if i < tabCount-1 {
			b.WriteString(t.Dim.Render(" ┊ "))
		}
	}
	if m.focusRight {
		b.WriteString(t.Dim.Render("   [/] tabs · tab back"))
	}
	s := b.String()
	if gap := width - lipgloss.Width(s); gap > 0 {
		s += strings.Repeat(" ", gap)
	}
	return s
}
