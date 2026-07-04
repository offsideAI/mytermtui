// Package ui implements the Bubble Tea program: an Elm-architecture
// model whose update loop never touches the disk directly — all I/O runs
// in commands and returns as messages.
package ui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/offsideai/mytermtui/internal/config"
	"github.com/offsideai/mytermtui/internal/fsx"
	"github.com/offsideai/mytermtui/internal/icloud"
)

type statusLevel int

const (
	levelInfo statusLevel = iota
	levelOK
	levelWarn
	levelErr
)

type statusNote struct {
	text  string
	level statusLevel
	id    int
}

type clipboard struct {
	paths []string
	cut   bool
}

type opState struct {
	desc string
	prog *fsx.Progress
}

// navIntent records an in-flight navigation so history stacks are only
// mutated once the destination actually loads (a failed load must not
// corrupt back/forward state).
type navKind int

const (
	navNone navKind = iota
	navPush
	navBack
	navForward
)

type navIntent struct {
	kind navKind
	from string // cwd when the navigation was issued
	to   string // destination being loaded
}

// Model is the root Bubble Tea model.
type Model struct {
	cfg    config.Config
	theme  Theme
	keys   Keymap
	bridge icloud.Bridge
	queue  *icloud.Queue

	w, h  int
	ready bool

	cwd      string
	home     string
	entries  []fsx.Entry
	view     []int // indexes into entries, filtered + sorted
	cursor   int   // index into view
	offset   int
	selected map[string]bool
	anchor   int // visual range anchor (index into view), -1 when off

	sortBy     fsx.SortBy
	sortAsc    bool
	dirsFirst  bool
	showHidden bool

	filterText  string
	filterInput textinput.Model
	filtering   bool

	histBack   []string
	histFwd    []string
	pendingNav navIntent
	loading    bool

	other      *paneState // parked panel when the dual-panel view is open
	focusRight bool       // which side the live fields represent
	ratio      float64    // left panel's share of the width

	previewOn bool
	hintsOn   bool
	prevPath  string
	prevLines []string

	clip clipboard
	undo *fsx.Undo

	qsnap   icloud.Snapshot
	qstate  map[string]icloud.ItemState // path → most relevant queue state
	qdirs   map[string]icloud.ItemState // ancestor dir → aggregate state
	ticking bool
	tickN   int

	op *opState

	menu  menuState
	modal Modal

	status    statusNote
	statusSeq int
}

// New builds the model. startDir must be absolute.
func New(cfg config.Config, startDir string, bridge icloud.Bridge, queue *icloud.Queue) *Model {
	home, _ := os.UserHomeDir()
	fi := textinput.New()
	fi.Prompt = "filter: "
	fi.CharLimit = 256
	m := &Model{
		cfg:         cfg,
		theme:       NewTheme(cfg.Theme.Name),
		keys:        BuildKeymap(cfg.Keys),
		bridge:      bridge,
		queue:       queue,
		cwd:         startDir,
		home:        home,
		selected:    map[string]bool{},
		anchor:      -1,
		sortBy:      fsx.SortName,
		sortAsc:     true,
		dirsFirst:   cfg.General.DirsFirst,
		showHidden:  cfg.General.ShowHidden,
		hintsOn:     cfg.General.ShowHints,
		ratio:       cfg.General.SplitRatio,
		filterInput: fi,
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{loadDirCmd(m.cwd, "")}
	m.loading = true
	if m.queue != nil && m.queue.HasActive() {
		m.ticking = true
		cmds = append(cmds, m.queueTickCmd())
	}
	return tea.Batch(cmds...)
}

// --- messages ---------------------------------------------------------------

type dirLoadedMsg struct {
	path       string
	entries    []fsx.Entry
	err        error
	keepCursor string
}

type opDoneMsg struct {
	desc    string
	err     error
	undo    *fsx.Undo
	refresh bool
}

type opTickMsg struct{}
type queueTickMsg struct{}

type previewMsg struct {
	path  string
	lines []string
}

type findResultsMsg struct {
	query   string
	results []fsx.FindResult
	capped  bool
}

type summaryMsg struct {
	root   string
	sum    icloud.Summary
	capped bool
	err    error
}

type marksExpandedMsg struct {
	files  []string
	total  int64
	capped bool
}

type statusExpireMsg struct{ id int }

type execDoneMsg struct {
	what string
	err  error
}

// --- commands ---------------------------------------------------------------

func loadDirCmd(path, keepCursor string) tea.Cmd {
	return func() tea.Msg {
		entries, err := fsx.ReadDir(path)
		return dirLoadedMsg{path: path, entries: entries, err: err, keepCursor: keepCursor}
	}
}

func (m *Model) queueTickCmd() tea.Cmd {
	d := time.Duration(m.cfg.ICloud.PollIntervalMs) * time.Millisecond
	return tea.Tick(d, func(time.Time) tea.Msg { return queueTickMsg{} })
}

func opTickCmd() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg { return opTickMsg{} })
}

// opCmd runs a mutating operation off the update loop.
func opCmd(desc string, refresh bool, fn func() (*fsx.Undo, error)) tea.Cmd {
	return func() tea.Msg {
		undo, err := fn()
		return opDoneMsg{desc: desc, err: err, undo: undo, refresh: refresh}
	}
}

func (m *Model) note(level statusLevel, text string) tea.Cmd {
	m.statusSeq++
	m.status = statusNote{text: text, level: level, id: m.statusSeq}
	id := m.statusSeq
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return statusExpireMsg{id} })
}

// --- update -----------------------------------------------------------------

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.ready = true
		m.clampCursor()
		return m, nil

	case tea.KeyMsg:
		return m.updateKey(msg)

	case dirLoadedMsg:
		m.loading = false
		if msg.err != nil {
			if m.pendingNav.to == msg.path {
				m.pendingNav = navIntent{} // failed nav: history untouched
			}
			cmd := m.note(levelErr, errHint(msg.err))
			return m, cmd
		}
		m.applyNav(msg.path)
		changedDir := msg.path != m.cwd
		m.cwd = msg.path
		m.entries = msg.entries
		// The old view indexes are meaningless against the new entries;
		// drop them before rebuildView touches currentEntry.
		if changedDir {
			m.view = nil // fresh slice: never truncate a possibly-parked array
			m.cursor = 0
			m.offset = 0
		}
		if changedDir {
			m.selected = map[string]bool{}
			m.anchor = -1
			m.filterText = ""
			m.filtering = false
			m.filterInput.SetValue("")
		}
		m.rebuildView(msg.keepCursor)
		return m, m.schedulePreview()

	case opDoneMsg:
		m.op = nil
		var cmds []tea.Cmd
		if msg.err != nil {
			cmds = append(cmds, m.note(levelErr, msg.desc+": "+errHint(msg.err)))
		} else {
			if msg.undo != nil {
				m.undo = msg.undo
			}
			cmds = append(cmds, m.note(levelOK, msg.desc))
		}
		if msg.refresh {
			cmds = append(cmds, m.reload())
		}
		return m, tea.Batch(cmds...)

	case opTickMsg:
		if m.op != nil {
			m.tickN++
			return m, opTickCmd()
		}
		return m, nil

	case queueTickMsg:
		prevDone := m.qsnap.DoneN
		m.setQsnap(m.queue.Tick())
		m.tickN++
		var cmds []tea.Cmd
		needReload := m.qsnap.DoneN != prevDone
		if m.queue.HasActive() {
			cmds = append(cmds, m.queueTickCmd())
		} else {
			m.ticking = false
			needReload = true
		}
		if needReload {
			cmds = append(cmds, m.reload())
		} else {
			m.refreshVisibleStats()
		}
		return m, tea.Batch(cmds...)

	case previewMsg:
		if msg.path == m.prevPath {
			m.prevLines = msg.lines
		}
		return m, nil

	case findResultsMsg:
		if fm, ok := m.modal.(*FindModal); ok {
			fm.absorb(msg)
		}
		return m, nil

	case summaryMsg:
		if msg.err != nil {
			return m, m.note(levelErr, "summary: "+errHint(msg.err))
		}
		m.modal = newSummaryModal(msg)
		return m, nil

	case marksExpandedMsg:
		if len(msg.files) == 0 {
			return m, m.note(levelInfo, "nothing to download — already local")
		}
		n := m.queue.Add(msg.files...)
		m.setQsnap(m.queue.Snapshot())
		note := ""
		if msg.capped {
			note = " (directory scan capped)"
		}
		cmds := []tea.Cmd{m.note(levelOK, "queued "+strconv.Itoa(n)+" file(s), "+humanBytes(msg.total)+note)}
		if !m.ticking && m.queue.HasActive() {
			m.ticking = true
			cmds = append(cmds, m.queueTickCmd())
		}
		return m, tea.Batch(cmds...)

	case statusExpireMsg:
		if m.status.id == msg.id {
			m.status = statusNote{}
		}
		return m, nil

	case execDoneMsg:
		if msg.err != nil {
			return m, m.note(levelErr, msg.what+": "+errHint(msg.err))
		}
		return m, nil
	}
	return m, nil
}

func (m *Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Modal captures everything.
	if m.modal != nil {
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Update(m, msg)
		return m, cmd
	}

	// Menu bar navigation.
	if m.menu.open {
		return m, m.updateMenu(key)
	}

	// Filter input mode.
	if m.filtering {
		switch key {
		case "esc":
			m.filtering = false
			m.filterText = ""
			m.filterInput.SetValue("")
			m.rebuildView(m.cursorPath())
			return m, nil
		case "enter":
			m.filtering = false
			return m, nil
		default:
			var cmd tea.Cmd
			m.filterInput, cmd = m.filterInput.Update(msg)
			m.filterText = m.filterInput.Value()
			m.rebuildView("")
			return m, cmd
		}
	}

	if key == "esc" {
		switch {
		case m.anchor >= 0:
			m.commitRange()
		case len(m.selected) > 0:
			m.selected = map[string]bool{}
		case m.filterText != "":
			m.filterText = ""
			m.filterInput.SetValue("")
			m.rebuildView(m.cursorPath())
		}
		return m, nil
	}

	if act, ok := m.keys.Lookup(key); ok {
		// In the right panel, ← (and h) steps focus back to the left
		// panel, column-style; backspace still goes to the parent.
		if act == ActParent && m.split() && m.focusRight && key != "backspace" {
			return m, m.dispatch(ActSwapPane)
		}
		return m, m.dispatch(act)
	}
	return m, nil
}

// --- view state helpers -------------------------------------------------

func (m *Model) rebuildView(keepPath string) {
	// Capture the cursor target before view indexes go stale. Only valid
	// when entries were not replaced (sort/filter/hidden toggles).
	if keepPath == "" {
		keepPath = m.cursorPath()
	}
	// A numeric range anchor cannot survive reindexing: keeping it would
	// silently re-target the range onto different files.
	m.anchor = -1
	fsx.Sort(m.entries, m.sortBy, m.sortAsc, m.dirsFirst)
	m.view = make([]int, 0, len(m.entries))
	needle := strings.ToLower(m.filterText)
	for i, e := range m.entries {
		if !m.showHidden && e.Hidden {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(e.Name), needle) {
			continue
		}
		m.view = append(m.view, i)
	}
	m.cursor = 0
	if keepPath != "" {
		for vi, ei := range m.view {
			if m.entries[ei].Path == keepPath {
				m.cursor = vi
				break
			}
		}
	}
	m.clampCursor()
	m.prevPath = ""
	m.prevLines = nil
}

func (m *Model) cursorPath() string {
	if e := m.currentEntry(); e != nil {
		return e.Path
	}
	return ""
}

func (m *Model) currentEntry() *fsx.Entry {
	if m.cursor < 0 || m.cursor >= len(m.view) {
		return nil
	}
	ei := m.view[m.cursor]
	if ei < 0 || ei >= len(m.entries) {
		return nil // view is stale relative to entries (mid-reload)
	}
	return &m.entries[ei]
}

// setQsnap stores a queue snapshot and rebuilds the path→state index the
// renderer uses, keeping row-glyph lookup O(1) even with huge queues.
// Active states are also aggregated onto every ancestor directory inside
// iCloud, so a folder whose *contents* are queued or downloading carries
// the ◌/⇣ glyph itself.
func (m *Model) setQsnap(s icloud.Snapshot) {
	m.qsnap = s
	m.qstate = make(map[string]icloud.ItemState, len(s.Items))
	m.qdirs = map[string]icloud.ItemState{}
	root := icloud.MobileDocs()
	// The ancestor fan-out is bounded: for absurdly large queues, skip
	// aggregation rather than burn CPU every tick (folders degrade to ·).
	aggregate := len(s.Items) <= 20000
	for _, it := range s.Items {
		if cur, ok := m.qstate[it.Path]; ok && activeState(cur) && !activeState(it.State) {
			continue // an active entry for the path outranks finished ones
		}
		m.qstate[it.Path] = it.State
		if !aggregate || !activeState(it.State) {
			continue
		}
		state := icloud.StateQueued
		if it.State != icloud.StateQueued {
			state = icloud.StateDownloading // any in-flight descendant
		}
		for dir := filepath.Dir(it.Path); strings.HasPrefix(dir, root); dir = filepath.Dir(dir) {
			if cur, ok := m.qdirs[dir]; ok && (state == icloud.StateQueued || cur == icloud.StateDownloading) {
				continue // transferring outranks queued; no downgrade
			}
			m.qdirs[dir] = state
		}
	}
}

func activeState(s icloud.ItemState) bool {
	switch s {
	case icloud.StateQueued, icloud.StateStarting, icloud.StateDownloading, icloud.StateStalled:
		return true
	}
	return false
}

// applyNav commits the pending history mutation once its destination has
// actually loaded.
func (m *Model) applyNav(loaded string) {
	nav := m.pendingNav
	if nav.kind == navNone || nav.to != loaded {
		return
	}
	m.pendingNav = navIntent{}
	switch nav.kind {
	case navPush:
		m.histBack = append(m.histBack, nav.from)
		m.histFwd = nil
	case navBack:
		if n := len(m.histBack); n > 0 && m.histBack[n-1] == nav.to {
			m.histBack = m.histBack[:n-1]
		}
		m.histFwd = append(m.histFwd, nav.from)
	case navForward:
		if n := len(m.histFwd); n > 0 && m.histFwd[n-1] == nav.to {
			m.histFwd = m.histFwd[:n-1]
		}
		m.histBack = append(m.histBack, nav.from)
	}
}

func (m *Model) listHeight() int {
	h := m.h - 4 - m.hintBarH() // menubar, breadcrumb, header, status bar, hints
	if m.downloadBarVisible() {
		h--
	}
	if m.filtering || m.filterText != "" {
		h--
	}
	if h < 1 {
		h = 1
	}
	return h
}

// hintBarH is the height of the nano-style shortcut box: two hint rows
// plus the box border. Auto-hidden on very short terminals.
func (m *Model) hintBarH() int {
	if !m.hintsOn || m.h < 16 {
		return 0
	}
	return 4
}

func (m *Model) downloadBarVisible() bool {
	return m.qsnap.ActiveN > 0 || (m.ticking && m.qsnap.TotalN > 0)
}

func (m *Model) clampCursor() {
	if m.cursor >= len(m.view) {
		m.cursor = len(m.view) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	lh := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+lh {
		m.offset = m.cursor - lh + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *Model) moveCursor(delta int) tea.Cmd {
	if len(m.view) == 0 {
		return nil
	}
	m.cursor += delta
	m.clampCursor()
	return m.schedulePreview()
}

// commitRange folds the active visual range into the selection set.
func (m *Model) commitRange() {
	if m.anchor < 0 {
		return
	}
	lo, hi := m.anchor, m.cursor
	if lo > hi {
		lo, hi = hi, lo
	}
	for vi := lo; vi <= hi && vi < len(m.view); vi++ {
		m.selected[m.entries[m.view[vi]].Path] = true
	}
	m.anchor = -1
}

// inRange reports whether view index vi falls inside the visual range.
func (m *Model) inRange(vi int) bool {
	if m.anchor < 0 {
		return false
	}
	lo, hi := m.anchor, m.cursor
	if lo > hi {
		lo, hi = hi, lo
	}
	return vi >= lo && vi <= hi
}

// selectionPaths returns marked paths in view order, falling back to the
// cursor entry. The visual range is materialized first.
func (m *Model) selectionPaths() []string {
	m.commitRange()
	var out []string
	for _, ei := range m.view {
		if m.selected[m.entries[ei].Path] {
			out = append(out, m.entries[ei].Path)
		}
	}
	if len(out) == 0 {
		if e := m.currentEntry(); e != nil {
			out = []string{e.Path}
		}
	}
	return out
}

// selectionEntries resolves selectionPaths back to entries where possible.
func (m *Model) selectionEntries() []fsx.Entry {
	paths := m.selectionPaths()
	byPath := map[string]fsx.Entry{}
	for _, e := range m.entries {
		byPath[e.Path] = e
	}
	var out []fsx.Entry
	for _, p := range paths {
		if e, ok := byPath[p]; ok {
			out = append(out, e)
		}
	}
	return out
}

func (m *Model) reload() tea.Cmd {
	m.loading = true
	return loadDirCmd(m.cwd, m.cursorPath())
}

// refreshVisibleStats re-lstats only the rows on screen — cheap enough to
// run on every queue tick so glyphs update while downloads land.
func (m *Model) refreshVisibleStats() {
	lh := m.listHeight()
	for vi := m.offset; vi < m.offset+lh && vi < len(m.view); vi++ {
		e := &m.entries[m.view[vi]]
		fi, err := os.Lstat(e.Path)
		if err != nil {
			continue
		}
		e.Size = fi.Size()
		e.Dataless = icloud.Dataless(fi)
		e.LocalBytes = icloud.LocalBytes(fi)
	}
}

// navigateTo loads path as a forward navigation. History is committed in
// applyNav only after the load succeeds.
func (m *Model) navigateTo(path, keep string) tea.Cmd {
	path = filepath.Clean(path)
	if path != m.cwd {
		m.pendingNav = navIntent{kind: navPush, from: m.cwd, to: path}
	}
	m.loading = true
	return loadDirCmd(path, keep)
}

// errHint compacts common errors and adds the Full Disk Access hint.
func errHint(err error) string {
	s := err.Error()
	if os.IsPermission(err) {
		s += " — grant Full Disk Access to your terminal in System Settings → Privacy & Security"
	}
	return s
}
