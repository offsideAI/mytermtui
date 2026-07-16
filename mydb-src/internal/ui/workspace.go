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
	tabSQL
	tabCount
)

var tabNames = [tabCount]string{"Info", "Data", "SQL"}

// swapFocus toggles which side receives keys.
func (m *Model) swapFocus() tea.Cmd {
	if !m.panelOn {
		m.panelOn = true
	}
	m.focusRight = !m.focusRight
	if !m.focusRight {
		return nil
	}
	switch m.tab {
	case tabData:
		return m.maybeLoadGrid()
	case tabSQL:
		m.currentSQLSession(true)
	}
	return nil
}

// switchTab activates a workspace tab (with wraparound).
func (m *Model) switchTab(delta int) tea.Cmd {
	m.tab = (m.tab + delta + tabCount) % tabCount
	switch m.tab {
	case tabData:
		return m.maybeLoadGrid()
	case tabSQL:
		if m.focusRight {
			m.currentSQLSession(true)
		}
	}
	return nil
}

// workspaceKey handles keys while the workspace is focused.
func (m *Model) workspaceKey(msg tea.KeyMsg) tea.Cmd {
	if m.tab == tabSQL {
		return m.sqlTabKey(msg)
	}
	key := msg.String()
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
	case "ctrl+h":
		return m.openHistory()
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
	case tabSQL:
		lines = append(lines, m.renderSQLTab(width, body)...)
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
		label := name
		if i == tabSQL {
			if s := m.currentSQLSession(false); s != nil && s.running {
				label += " " + spinnerFrames[m.tickN%len(spinnerFrames)]
			}
		}
		b.WriteString(style.Render(label))
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
