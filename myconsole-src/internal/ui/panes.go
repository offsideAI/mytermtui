package ui

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/offsideai/myconsole/internal/fsx"
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

// Explorer model: the live pane is the tree (left) unless focusRight,
// in which case the live pane is the contents panel and the tree is
// parked in m.other. The contents panel auto-follows the tree cursor.

// followCursor keeps the passive right side in sync with the tree
// cursor: files feed the preview lines, folders load a contents pane.
func (m *Model) followCursor() tea.Cmd {
	return tea.Batch(m.schedulePreview(), m.scheduleContents())
}

// scheduleContents loads the selected folder's listing for the right
// panel when it is stale.
func (m *Model) scheduleContents() tea.Cmd {
	if !m.previewOn || m.focusRight {
		return nil
	}
	e := m.currentEntry()
	if e == nil || !e.IsDir {
		return nil
	}
	if m.other != nil && m.other.cwd == e.Path {
		return nil
	}
	return loadContentsCmd(e.Path, false, "")
}

// makeContentsPane builds a flat, parked listing for the right panel,
// honoring the current sort and hidden settings.
func (m *Model) makeContentsPane(path string, entries []fsx.Entry, cursorOn string) *paneState {
	flat := append([]fsx.Entry(nil), entries...)
	fsx.Sort(flat, m.sortBy, m.sortAsc, m.dirsFirst)
	var out []fsx.Entry
	for _, e := range flat {
		if !m.showHidden && e.Hidden {
			continue
		}
		out = append(out, e)
	}
	p := &paneState{
		cwd:         path,
		rootEntries: entries,
		entries:     out,
		expanded:    map[string]bool{},
		childCache:  map[string][]fsx.Entry{},
		selected:    map[string]bool{},
	}
	p.view = make([]int, len(out))
	for i := range out {
		p.view[i] = i
	}
	for vi, ei := range p.view {
		if p.entries[ei].Path == cursorOn {
			p.cursor = vi
			break
		}
	}
	return p
}

// smartRightAction handles →/l, Windows-Explorer style. In the tree:
// collapsed folder → expand (+ → −); expanded folder → step to its
// first child; file → focus the contents panel. In the contents panel:
// folder → navigate into it.
func (m *Model) smartRightAction() tea.Cmd {
	e := m.currentEntry()
	if e == nil {
		return nil
	}
	if m.focusRight {
		if e.IsDir {
			return m.navigateTo(e.Path, "")
		}
		return nil
	}
	if e.IsDir {
		if !m.expanded[e.Path] {
			return m.toggleExpandAction(*e)
		}
		if m.cursor+1 < len(m.view) {
			if next := m.entries[m.view[m.cursor+1]]; next.Depth == e.Depth+1 {
				return m.moveCursor(1)
			}
		}
		return nil
	}
	return m.swapPaneAction()
}

// smartLeftAction handles ←/h in the tree: expanded folder → collapse
// (− → +); nested row → jump to its parent row; top-level row → re-root
// the tree at the parent directory.
func (m *Model) smartLeftAction() tea.Cmd {
	e := m.currentEntry()
	if e != nil && e.IsDir && m.expanded[e.Path] {
		delete(m.expanded, e.Path)
		m.rebuildView(e.Path)
		return m.followCursor()
	}
	if e != nil && e.Depth > 0 {
		parent := filepath.Dir(e.Path)
		for vi := m.cursor - 1; vi >= 0; vi-- {
			if m.entries[m.view[vi]].Path == parent {
				m.cursor = vi
				m.clampCursor()
				return m.followCursor()
			}
		}
	}
	parent := filepath.Dir(m.cwd)
	if parent == m.cwd {
		return nil
	}
	return m.navigateTo(parent, m.cwd)
}

// swapPaneAction handles tab: focus flips between the tree and the
// contents panel; each side keeps its cursor and selection.
func (m *Model) swapPaneAction() tea.Cmd {
	if m.editor != nil {
		m.editorFocused = true
		return nil
	}
	if !m.previewOn {
		return m.note(levelInfo, "contents panel hidden — press F3 to show it")
	}
	if m.focusRight {
		cur := m.snapshotPane()
		m.restorePane(m.other)
		m.other = cur
		m.focusRight = false
		return m.followCursor()
	}
	e := m.currentEntry()
	target, cursorOn := m.cwd, ""
	if e != nil {
		if e.IsDir {
			target = e.Path
		} else {
			target = filepath.Dir(e.Path)
			cursorOn = e.Path
		}
	}
	if m.other != nil && m.other.cwd == target && cursorOn == "" {
		cur := m.snapshotPane()
		m.restorePane(m.other)
		m.other = cur
		m.focusRight = true
		return m.reload()
	}
	return loadContentsCmd(target, true, cursorOn)
}

// togglePanelAction shows/hides the contents panel (F3 / ctrl+w),
// returning focus to the tree first when needed.
func (m *Model) togglePanelAction() tea.Cmd {
	if m.previewOn && m.focusRight {
		cur := m.snapshotPane()
		m.restorePane(m.other)
		m.other = cur
		m.focusRight = false
	}
	m.previewOn = !m.previewOn
	m.clampCursor()
	if m.previewOn {
		return m.followCursor()
	}
	return nil
}

// resizePaneAction adjusts the tree's share of the width.
func (m *Model) resizePaneAction(delta float64) tea.Cmd {
	if !m.previewOn {
		return m.note(levelInfo, "no panel to resize — press F3 to show it")
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
