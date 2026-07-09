package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/offsideai/myterm/internal/fsx"
)

// Dual-panel navigation: → (or l) on a folder opens it in a right panel
// while the current listing docks on the left; tab swaps focus, each
// panel keeping its own cursor, selection, filter, and history. The
// Model's flat fields always describe the *focused* panel; the inactive
// one is parked in a paneState and swapped wholesale on focus changes.

// paneState is a parked panel: everything a listing needs to resume.
type paneState struct {
	cwd         string
	entries     []fsx.Entry
	view        []int
	rootEntries []fsx.Entry
	expanded    map[string]bool
	childCache  map[string][]fsx.Entry
	cursor      int
	offset      int
	selected    map[string]bool
	filterText  string
	histBack    []string
	histFwd     []string
}

// split reports whether the two-panel layout is active.
func (m *Model) split() bool { return m.other != nil }

// snapshotPane parks the live (focused) panel.
func (m *Model) snapshotPane() *paneState {
	m.commitRange() // a numeric anchor cannot survive parking
	return &paneState{
		cwd:         m.cwd,
		entries:     m.entries,
		view:        m.view,
		rootEntries: m.rootEntries,
		expanded:    m.expanded,
		childCache:  m.childCache,
		cursor:      m.cursor,
		offset:      m.offset,
		selected:    m.selected,
		filterText:  m.filterText,
		histBack:    m.histBack,
		histFwd:     m.histFwd,
	}
}

// restorePane makes a parked panel the live one.
func (m *Model) restorePane(p *paneState) {
	m.cwd = p.cwd
	m.entries = p.entries
	m.view = p.view
	m.rootEntries = p.rootEntries
	m.expanded = p.expanded
	m.childCache = p.childCache
	m.cursor = p.cursor
	m.offset = p.offset
	m.selected = p.selected
	m.filterText = p.filterText
	m.filterInput.SetValue(p.filterText)
	m.histBack = p.histBack
	m.histFwd = p.histFwd
	m.anchor = -1
	m.filtering = false
	m.pendingNav = navIntent{}
	m.prevPath = ""
	m.prevLines = nil
	m.clampCursor()
}

// resetLivePane clears the live panel for a fresh directory load.
func (m *Model) resetLivePane() {
	m.entries = nil
	// Fresh slices/maps, not truncations: [:0] would alias the parked
	// panel's backing array and let the next rebuild corrupt it.
	m.view = nil
	m.rootEntries = nil
	m.expanded = map[string]bool{}
	m.childCache = map[string][]fsx.Entry{}
	m.cursor = 0
	m.offset = 0
	m.selected = map[string]bool{}
	m.anchor = -1
	m.filterText = ""
	m.filterInput.SetValue("")
	m.filtering = false
	m.histBack = nil
	m.histFwd = nil
	m.pendingNav = navIntent{}
	m.prevPath = ""
	m.prevLines = nil
}

// openRightAction handles →/l: folders open in the right panel (focused
// there); files behave exactly like enter.
func (m *Model) openRightAction() tea.Cmd {
	e := m.currentEntry()
	if e == nil {
		return nil
	}
	if !e.IsDir {
		return m.openFileAction(*e)
	}
	dest := e.Path
	// The current panel parks on the left (or, cascading from an
	// already-focused right panel, takes the left slot).
	m.other = m.snapshotPane()
	m.focusRight = true
	m.resetLivePane()
	m.loading = true
	return loadDirCmd(dest, "")
}

// swapPaneAction handles tab: exchange live and parked panels.
func (m *Model) swapPaneAction() tea.Cmd {
	if !m.split() {
		return m.note(levelInfo, "no second panel — press → on a folder to open one")
	}
	cur := m.snapshotPane()
	m.restorePane(m.other)
	m.other = cur
	m.focusRight = !m.focusRight
	return m.reload() // freshen the newly focused panel, keeping its cursor
}

// closePaneAction handles ctrl+w: collapse back to a single panel. The
// left panel survives; if the right one was focused, focus moves left.
func (m *Model) closePaneAction() tea.Cmd {
	if !m.split() {
		return nil
	}
	if m.focusRight {
		m.restorePane(m.other) // left panel becomes the view
	}
	m.other = nil
	m.focusRight = false
	return m.schedulePreview()
}

// resizePaneAction adjusts the left share of the width — of the
// dual-panel split, or of the list/detail divide when the detail panel
// is showing.
func (m *Model) resizePaneAction(delta float64) tea.Cmd {
	if !m.split() && !m.previewOn {
		return m.note(levelInfo, "nothing to resize — no split or detail panel")
	}
	m.ratio += delta
	if m.ratio < 0.15 {
		m.ratio = 0.15
	}
	if m.ratio > 0.85 {
		m.ratio = 0.85
	}
	return nil
}

// paneRef is a read-only view of one panel for rendering.
type paneRef struct {
	cwd      string
	entries  []fsx.Entry
	view     []int
	cursor   int
	offset   int
	selected map[string]bool
	expanded map[string]bool
	anchor   int
	filter   string
	live     bool
}

func (m *Model) liveRef() paneRef {
	return paneRef{
		cwd: m.cwd, entries: m.entries, view: m.view,
		cursor: m.cursor, offset: m.offset, selected: m.selected,
		expanded: m.expanded, anchor: m.anchor, filter: m.filterText, live: true,
	}
}

func refOf(p *paneState) paneRef {
	return paneRef{
		cwd: p.cwd, entries: p.entries, view: p.view,
		cursor: p.cursor, offset: p.offset, selected: p.selected,
		expanded: p.expanded, anchor: -1, filter: p.filterText,
	}
}

// refInRange mirrors Model.inRange for a rendered ref.
func (r paneRef) inRange(vi int) bool {
	if r.anchor < 0 {
		return false
	}
	lo, hi := r.anchor, r.cursor
	if lo > hi {
		lo, hi = hi, lo
	}
	return vi >= lo && vi <= hi
}
