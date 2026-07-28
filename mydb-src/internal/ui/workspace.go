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
	tabJobs
	tabCount
)

var tabNames = [tabCount]string{"Info", "Data", "SQL", "Jobs"}

// enterTab prepares a tab when focus lands on it (load the grid, ensure
// the SQL session exists).
func (m *Model) enterTab(tab int) tea.Cmd {
	switch tab {
	case tabData:
		return m.maybeLoadGrid()
	case tabSQL:
		if s := m.currentSQLSession(true); s != nil {
			s.editorFocused = true
		}
	}
	return nil
}

// swapFocus toggles focus between the tree and the workspace, preserving
// the current tab. Bound to ctrl+w / ctrl+o — the quick jump that also
// works from the SQL editor's insert mode (where Tab types indentation).
func (m *Model) swapFocus() tea.Cmd {
	if !m.panelOn {
		m.panelOn = true
	}
	m.focusRight = !m.focusRight
	if !m.focusRight {
		return nil
	}
	return m.enterTab(m.tab)
}

// walkFocus moves focus forward one step, so Tab walks through everything
// left-to-right: tree → Info → Data → SQL(editor → results) → Jobs → tree,
// then wraps. This makes the universally-delivered Tab key reach every
// tab (some terminals eat ctrl+w), matching the visible tab strip.
func (m *Model) walkFocus() tea.Cmd {
	if !m.panelOn {
		m.panelOn = true
	}
	if !m.focusRight {
		m.focusRight = true
		return m.enterTab(m.tab)
	}
	// Inside the SQL tab, Tab steps editor → results, then (on the next
	// Tab) leaves the tab — resetting the sub-focus for the next visit.
	if m.tab == tabSQL {
		if s := m.currentSQLSession(false); s != nil {
			if s.editorFocused {
				s.editorFocused = false // editor → results, stay on SQL
				return nil
			}
			s.editorFocused = true // leaving SQL: next visit starts in the editor
		}
	}
	if m.tab >= tabCount-1 {
		// Past the last tab: back to the tree, and reset to the first tab
		// so the next Tab restarts the walk from the top.
		m.focusRight = false
		m.tab = tabInfo
		return nil
	}
	m.tab++
	return m.enterTab(m.tab)
}

// switchTab activates a workspace tab directly (bound to [ / ]).
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
	case "tab":
		return m.walkFocus()
	case "ctrl+w", "ctrl+o", "esc":
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
	switch m.tab {
	case tabData:
		return m.gridKey(key)
	case tabJobs:
		return m.jobsKey(key)
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
	case tabJobs:
		lines = append(lines, m.renderJobsTab(width, body)...)
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
