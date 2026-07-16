package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/offsideai/mydb/internal/registry"
)

// HistoryModal: fuzzy-searchable query history. Enter loads the selected
// entry into the current connection's SQL editor.
type HistoryModal struct {
	input    textinput.Model
	connID   int64 // scope connection (0 = all)
	scopeAll bool
	loaded   bool
	entries  []registry.HistoryEntry
	filtered []int
	cursor   int
}

type historyLoadedMsg struct {
	entries []registry.HistoryEntry
}

func historyLoadCmd(reg *registry.Registry, connID int64) tea.Cmd {
	return func() tea.Msg {
		entries, err := reg.History(connID, 500)
		if err != nil {
			entries = nil
		}
		return historyLoadedMsg{entries: entries}
	}
}

// openHistory opens the modal scoped to the cursor's connection.
func (m *Model) openHistory() tea.Cmd {
	ti := textinput.New()
	ti.Prompt = "search: "
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 44
	hm := &HistoryModal{input: ti}
	if c := m.cursorConn(); c != nil {
		hm.connID = c.ID
	} else {
		hm.scopeAll = true
	}
	m.modal = hm
	return historyLoadCmd(m.reg, hm.scope())
}

func (h *HistoryModal) scope() int64 {
	if h.scopeAll {
		return 0
	}
	return h.connID
}

func (h *HistoryModal) absorb(msg historyLoadedMsg) {
	h.loaded = true
	h.entries = msg.entries
	h.refilter()
}

// refilter applies the fuzzy query over the loaded entries.
func (h *HistoryModal) refilter() {
	needle := strings.TrimSpace(h.input.Value())
	h.filtered = h.filtered[:0]
	for i, e := range h.entries {
		if needle == "" || fuzzyMatch(needle, e.SQL) || fuzzyMatch(needle, e.ConnName) {
			h.filtered = append(h.filtered, i)
		}
	}
	if h.cursor >= len(h.filtered) {
		h.cursor = len(h.filtered) - 1
	}
	if h.cursor < 0 {
		h.cursor = 0
	}
}

// fuzzyMatch is a case-insensitive subsequence match.
func fuzzyMatch(needle, hay string) bool {
	needle = strings.ToLower(needle)
	hay = strings.ToLower(hay)
	i := 0
	for _, r := range hay {
		if i < len(needle) && rune(needle[i]) == r {
			i++
		}
	}
	return i == len(needle)
}

func (h *HistoryModal) Update(m *Model, msg tea.KeyMsg) (Modal, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return nil, nil
	case "ctrl+t":
		h.scopeAll = !h.scopeAll
		h.loaded = false
		return h, historyLoadCmd(m.reg, h.scope())
	case "up", "ctrl+p":
		if h.cursor > 0 {
			h.cursor--
		}
		return h, nil
	case "down", "ctrl+n":
		if h.cursor < len(h.filtered)-1 {
			h.cursor++
		}
		return h, nil
	case "enter":
		if h.cursor < len(h.filtered) {
			e := h.entries[h.filtered[h.cursor]]
			return nil, m.loadHistoryEntry(e)
		}
		return nil, nil
	}
	var cmd tea.Cmd
	h.input, cmd = h.input.Update(msg)
	h.refilter()
	return h, cmd
}

// loadHistoryEntry replaces the current connection's editor buffer with
// the entry's SQL and focuses the SQL tab.
func (m *Model) loadHistoryEntry(e registry.HistoryEntry) tea.Cmd {
	s := m.currentSQLSession(true)
	if s == nil {
		return m.note(levelWarn, "select a connection first")
	}
	title := s.editor.path
	s.editor = newEditor(title, []byte(strings.TrimRight(e.SQL, "\n")), false)
	s.editorFocused = true
	m.panelOn = true
	m.focusRight = true
	m.tab = tabSQL
	return m.note(levelOK, "loaded from history — ctrl+r runs it")
}

func (h *HistoryModal) View(m *Model, width int) string {
	t := m.theme
	var b strings.Builder
	scope := "current connection"
	if h.scopeAll {
		scope = "all connections"
	}
	b.WriteString(t.ModalTitle.Render("Query history — "+scope) + "\n\n")
	b.WriteString(h.input.View() + "\n\n")

	switch {
	case !h.loaded:
		b.WriteString(t.ModalDim.Render("loading…") + "\n")
	case len(h.filtered) == 0:
		b.WriteString(t.ModalDim.Render("no matching queries") + "\n")
	}

	page := m.listHeight() - 8
	if page < 3 {
		page = 3
	}
	start := 0
	if h.cursor >= page {
		start = h.cursor - page + 1
	}
	for i := start; i < len(h.filtered) && i < start+page; i++ {
		e := h.entries[h.filtered[i]]
		mark := t.GlyphOpen.Render("✓")
		if !e.OK {
			mark = t.GlyphFailed.Render("✗")
		}
		sql := strings.Join(strings.Fields(e.SQL), " ") // one line
		line := fmt.Sprintf("%s %s  %s", mark,
			t.ModalDim.Render(pad(truncEnd(e.ConnName, 12), 12)),
			truncEnd(sanitize(sql), 46))
		if i == h.cursor {
			line = t.Cursor.Render(pad(fmt.Sprintf("✓ %s  %s",
				pad(truncEnd(e.ConnName, 12), 12), truncEnd(sanitize(sql), 46)), 64))
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + t.ModalDim.Render("enter load into editor · ctrl+t scope · esc close"))
	return b.String()
}
