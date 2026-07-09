package ui

import (
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Editor is a small modal (vim-style) text editor rendered inside the
// right panel. It implements a practical subset of vim — normal, insert,
// command-line, and search modes with the common motions and edits — not
// full vim (no plugins, no .vimrc, no visual mode). The buffer is a slice
// of rune-slices; edits mutate it in place and snapshot for undo.
type editorMode int

const (
	edNormal editorMode = iota
	edInsert
	edCommand // after ':'
	edSearch  // after '/'
)

type editorSnapshot struct {
	lines    [][]rune
	row, col int
}

// Editor holds the state of one open file.
type Editor struct {
	path     string
	lines    [][]rune
	row, col int // cursor; col is a rune index into lines[row]
	top      int // first visible line (vertical scroll)
	mode     editorMode
	modified bool
	readonly bool

	cmdline string // text typed after ':' or '/'
	status  string // transient message shown on the status line
	pending string // pending operator/prefix: "d","c","y","g","r"
	count   int    // numeric prefix (0 = none)

	yank   [][]rune // yanked lines (linewise)
	undo   []editorSnapshot
	redo   []editorSnapshot
	search string // last search pattern
	quit   bool   // set by :q/:q! — the Model closes the editor
}

// newEditor builds an editor over data (already known to be local text).
func newEditor(path string, data []byte, readonly bool) *Editor {
	text := string(data)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var lines [][]rune
	for _, l := range strings.Split(text, "\n") {
		lines = append(lines, []rune(l))
	}
	if len(lines) == 0 {
		lines = [][]rune{{}}
	}
	return &Editor{path: path, lines: lines, readonly: readonly}
}

// --- helpers ---------------------------------------------------------------

func (e *Editor) line() []rune { return e.lines[e.row] }

func (e *Editor) clampCol(insert bool) {
	max := len(e.line())
	if !insert && max > 0 {
		max-- // normal mode rests on the last char, not past it
	}
	if e.col > max {
		e.col = max
	}
	if e.col < 0 {
		e.col = 0
	}
}

func (e *Editor) clampRow() {
	if e.row >= len(e.lines) {
		e.row = len(e.lines) - 1
	}
	if e.row < 0 {
		e.row = 0
	}
}

func (e *Editor) snapshot() editorSnapshot {
	cp := make([][]rune, len(e.lines))
	for i, l := range e.lines {
		cp[i] = append([]rune(nil), l...)
	}
	return editorSnapshot{lines: cp, row: e.row, col: e.col}
}

// pushUndo records the pre-edit state; call once per logical change.
func (e *Editor) pushUndo() {
	e.undo = append(e.undo, e.snapshot())
	e.redo = nil
	e.modified = true
}

func (e *Editor) restore(s editorSnapshot) {
	e.lines = s.lines
	e.row, e.col = s.row, s.col
	e.clampRow()
	e.clampCol(false)
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

// takeCount returns the pending count (min 1) and resets it.
func (e *Editor) takeCount() int {
	n := e.count
	e.count = 0
	if n < 1 {
		return 1
	}
	return n
}

// Content returns the buffer as bytes for saving (trailing newline).
func (e *Editor) Content() []byte {
	var b strings.Builder
	for i, l := range e.lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(string(l))
	}
	b.WriteByte('\n')
	return []byte(b.String())
}

// --- update ----------------------------------------------------------------

// Update processes one key. It returns a command (e.g. a save) and never
// mutates anything outside the editor; the Model reads e.quit afterward.
func (e *Editor) Update(m *Model, msg tea.KeyMsg) tea.Cmd {
	e.status = ""
	switch e.mode {
	case edInsert:
		return e.updateInsert(msg)
	case edCommand:
		return e.updateCommand(m, msg)
	case edSearch:
		return e.updateSearch(msg)
	default:
		return e.updateNormal(m, msg)
	}
}

func (e *Editor) updateNormal(m *Model, msg tea.KeyMsg) tea.Cmd {
	key := msg.String()

	// A pending single-char operand: r<char>.
	if e.pending == "r" {
		e.pending = ""
		if len(msg.Runes) == 1 && !e.readonly {
			e.pushUndo()
			if e.col < len(e.line()) {
				e.line()[e.col] = msg.Runes[0]
			}
		}
		return nil
	}

	// Numeric count prefix (0 only extends an existing count).
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		e.count = e.count*10 + int(key[0]-'0')
		return nil
	}
	if key == "0" && e.count > 0 {
		e.count *= 10
		return nil
	}

	// An operator (d/c) awaiting a motion: dw, cw, de, d$, …
	if e.pending == "d" || e.pending == "c" {
		switch key {
		case "w", "e", "b", "$":
			e.applyOperatorMotion(key)
			e.clampRow()
			e.clampCol(false)
			e.scroll()
			return nil
		}
	}

	switch key {
	case "h", "left":
		e.moveCol(-e.takeCount())
	case "l", "right", " ":
		e.moveCol(e.takeCount())
	case "j", "down":
		e.moveRow(e.takeCount())
	case "k", "up":
		e.moveRow(-e.takeCount())
	case "0":
		e.col = 0
	case "$":
		e.col = len(e.line())
		e.clampCol(false)
	case "^":
		e.col = firstNonBlank(e.line())
	case "w":
		for n := e.takeCount(); n > 0; n-- {
			e.wordForward()
		}
	case "b":
		for n := e.takeCount(); n > 0; n-- {
			e.wordBack()
		}
	case "e":
		for n := e.takeCount(); n > 0; n-- {
			e.wordEnd()
		}
	case "G":
		if e.count > 0 {
			e.row = e.takeCount() - 1
		} else {
			e.row = len(e.lines) - 1
		}
		e.clampRow()
		e.col = firstNonBlank(e.line())
	case "g":
		if e.pending == "g" {
			e.pending = ""
			e.row = 0
			e.col = firstNonBlank(e.line())
		} else {
			e.pending = "g"
			return nil
		}

	// enter insert mode
	case "i":
		e.enterInsert()
	case "a":
		if len(e.line()) > 0 {
			e.col++
		}
		e.enterInsert()
	case "I":
		e.col = firstNonBlank(e.line())
		e.enterInsert()
	case "A":
		e.col = len(e.line())
		e.enterInsert()
	case "o":
		e.openLine(true)
	case "O":
		e.openLine(false)

	// edits
	case "x":
		e.deleteChars(e.takeCount())
	case "D":
		e.deleteToEol(false)
	case "C":
		e.deleteToEol(true)
	case "d":
		if e.pending == "d" {
			e.pending = ""
			e.deleteLines(e.takeCount())
		} else {
			e.pending = "d"
			return nil
		}
	case "c":
		if e.pending == "c" {
			e.pending = ""
			e.changeLine()
		} else {
			e.pending = "c"
			return nil
		}
	case "y":
		if e.pending == "y" {
			e.pending = ""
			e.yankLines(e.takeCount())
		} else {
			e.pending = "y"
			return nil
		}
	case "r":
		e.pending = "r"
		return nil
	case "p":
		e.paste(true)
	case "P":
		e.paste(false)
	case "u":
		e.doUndo()
	case "ctrl+r":
		e.doRedo()

	case ":":
		e.mode = edCommand
		e.cmdline = ""
	case "/":
		e.mode = edSearch
		e.cmdline = ""
	case "n":
		e.findNext(true)
	case "N":
		e.findNext(false)

	case "esc":
		e.pending = ""
		e.count = 0
	}

	e.clampRow()
	if e.mode == edNormal {
		e.clampCol(false)
	}
	e.scroll()
	return nil
}

func (e *Editor) applyOperatorMotion(motion string) {
	op := e.pending
	e.pending = ""
	if op == "c" && motion == "w" { // vim special case: cw == ce
		motion = "e"
	}
	start := e.col
	startRow := e.row
	switch motion {
	case "w":
		e.wordForward()
	case "e":
		e.wordEnd()
		e.col++ // end-inclusive
	case "b":
		e.wordBack()
	case "$":
		e.col = len(e.line())
	}
	if e.row != startRow { // a word motion that wrapped: clamp to line end
		e.row = startRow
		e.col = len(e.lines[startRow])
	}
	lo, hi := start, e.col
	if lo > hi {
		lo, hi = hi, lo
	}
	if hi > len(e.line()) {
		hi = len(e.line())
	}
	e.pushUndo()
	l := e.line()
	e.lines[e.row] = append(append([]rune(nil), l[:lo]...), l[hi:]...)
	e.col = lo
	if op == "c" {
		e.enterInsertNoSnapshot()
	}
}

func (e *Editor) updateInsert(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "ctrl+c":
		e.mode = edNormal
		if e.col > 0 {
			e.col--
		}
		e.clampCol(false)
	case "enter":
		l := e.line()
		rest := append([]rune(nil), l[e.col:]...)
		e.lines[e.row] = append([]rune(nil), l[:e.col]...)
		e.lines = insertLine(e.lines, e.row+1, rest)
		e.row++
		e.col = 0
	case "backspace", "ctrl+h":
		if e.col > 0 {
			l := e.line()
			e.lines[e.row] = append(append([]rune(nil), l[:e.col-1]...), l[e.col:]...)
			e.col--
		} else if e.row > 0 {
			prev := e.lines[e.row-1]
			e.col = len(prev)
			e.lines[e.row-1] = append(prev, e.line()...)
			e.lines = removeLine(e.lines, e.row)
			e.row--
		}
	case "tab":
		e.insertRunes([]rune("    "))
	default:
		if len(msg.Runes) > 0 {
			e.insertRunes(msg.Runes)
		}
	}
	e.scroll()
	return nil
}

func (e *Editor) updateCommand(m *Model, msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "ctrl+c":
		e.mode = edNormal
		e.cmdline = ""
	case "enter":
		cmd := e.runExCommand(m)
		e.mode = edNormal
		e.cmdline = ""
		return cmd
	case "backspace", "ctrl+h":
		if len(e.cmdline) > 0 {
			e.cmdline = e.cmdline[:len(e.cmdline)-1]
		} else {
			e.mode = edNormal
		}
	default:
		if len(msg.Runes) > 0 {
			e.cmdline += string(msg.Runes)
		}
	}
	return nil
}

func (e *Editor) updateSearch(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "ctrl+c":
		e.mode = edNormal
		e.cmdline = ""
	case "enter":
		e.search = e.cmdline
		e.mode = edNormal
		e.cmdline = ""
		e.findNext(true)
	case "backspace", "ctrl+h":
		if len(e.cmdline) > 0 {
			e.cmdline = e.cmdline[:len(e.cmdline)-1]
		} else {
			e.mode = edNormal
		}
	default:
		if len(msg.Runes) > 0 {
			e.cmdline += string(msg.Runes)
		}
	}
	return nil
}

// runExCommand handles :w :q :wq :x :q! and :w!.
func (e *Editor) runExCommand(m *Model) tea.Cmd {
	cmd := strings.TrimSpace(e.cmdline)
	write := false
	quit := false
	force := false
	switch cmd {
	case "w":
		write = true
	case "w!":
		write, force = true, true
	case "q":
		quit = true
	case "q!":
		quit, force = true, true
	case "wq", "x", "wq!", "x!":
		write, quit, force = true, true, strings.HasSuffix(cmd, "!")
	default:
		e.status = "not an editor command: " + cmd
		return nil
	}
	if write {
		if e.readonly && !force {
			e.status = "'readonly' option is set (add ! to override)"
			return nil
		}
		return saveEditorCmd(e.path, e.Content())
	}
	if quit {
		if e.modified && !force {
			e.status = "no write since last change (add ! to override)"
			return nil
		}
		e.quit = true
	}
	return nil
}

// --- motions ---------------------------------------------------------------

func (e *Editor) moveCol(d int) {
	e.col += d
	e.clampCol(false)
}

func (e *Editor) moveRow(d int) {
	e.row += d
	e.clampRow()
	e.clampCol(false)
}

func firstNonBlank(l []rune) int {
	for i, r := range l {
		if !unicode.IsSpace(r) {
			return i
		}
	}
	if len(l) == 0 {
		return 0
	}
	return len(l) - 1
}

func (e *Editor) wordForward() {
	l := e.line()
	i := e.col
	if i < len(l) && isWord(l[i]) {
		for i < len(l) && isWord(l[i]) {
			i++
		}
	} else if i < len(l) {
		for i < len(l) && !isWord(l[i]) && !unicode.IsSpace(l[i]) {
			i++
		}
	}
	for i < len(l) && unicode.IsSpace(l[i]) {
		i++
	}
	if i >= len(l) && e.row < len(e.lines)-1 {
		e.row++
		e.col = 0
		return
	}
	e.col = i
}

func (e *Editor) wordBack() {
	if e.col == 0 && e.row > 0 {
		e.row--
		e.col = len(e.line())
	}
	l := e.line()
	i := e.col - 1
	for i > 0 && unicode.IsSpace(l[i]) {
		i--
	}
	if i > 0 && isWord(l[i]) {
		for i > 0 && isWord(l[i-1]) {
			i--
		}
	}
	if i < 0 {
		i = 0
	}
	e.col = i
}

func (e *Editor) wordEnd() {
	l := e.line()
	i := e.col + 1
	for i < len(l) && unicode.IsSpace(l[i]) {
		i++
	}
	for i < len(l)-1 && isWord(l[i+1]) {
		i++
	}
	if i >= len(l) {
		i = len(l) - 1
	}
	if i < 0 {
		i = 0
	}
	e.col = i
}

// --- edits -----------------------------------------------------------------

func (e *Editor) enterInsert() {
	if e.readonly {
		e.status = "'readonly' option is set"
		return
	}
	e.pushUndo()
	e.mode = edInsert
	e.clampCol(true)
}

func (e *Editor) enterInsertNoSnapshot() {
	if e.readonly {
		return
	}
	e.mode = edInsert
	e.clampCol(true)
}

func (e *Editor) insertRunes(rs []rune) {
	l := e.line()
	nl := append([]rune(nil), l[:e.col]...)
	nl = append(nl, rs...)
	nl = append(nl, l[e.col:]...)
	e.lines[e.row] = nl
	e.col += len(rs)
}

func (e *Editor) openLine(below bool) {
	if e.readonly {
		e.status = "'readonly' option is set"
		return
	}
	e.pushUndo()
	at := e.row
	if below {
		at = e.row + 1
	}
	e.lines = insertLine(e.lines, at, []rune{})
	e.row = at
	e.col = 0
	e.mode = edInsert
}

func (e *Editor) deleteChars(n int) {
	if e.readonly || len(e.line()) == 0 {
		return
	}
	e.pushUndo()
	l := e.line()
	end := e.col + n
	if end > len(l) {
		end = len(l)
	}
	e.lines[e.row] = append(append([]rune(nil), l[:e.col]...), l[end:]...)
	e.clampCol(false)
}

func (e *Editor) deleteToEol(change bool) {
	if e.readonly {
		return
	}
	e.pushUndo()
	e.lines[e.row] = append([]rune(nil), e.line()[:e.col]...)
	if change {
		e.mode = edInsert
	} else {
		e.clampCol(false)
	}
}

func (e *Editor) deleteLines(n int) {
	if e.readonly {
		return
	}
	e.pushUndo()
	e.yankLinesNoStatus(n)
	end := e.row + n
	if end > len(e.lines) {
		end = len(e.lines)
	}
	e.lines = append(e.lines[:e.row], e.lines[end:]...)
	if len(e.lines) == 0 {
		e.lines = [][]rune{{}}
	}
	e.clampRow()
	e.col = firstNonBlank(e.line())
}

func (e *Editor) changeLine() {
	if e.readonly {
		return
	}
	e.pushUndo()
	e.lines[e.row] = []rune{}
	e.col = 0
	e.mode = edInsert
}

func (e *Editor) yankLines(n int) {
	e.yankLinesNoStatus(n)
	e.status = fmt.Sprintf("%d line(s) yanked", n)
}

func (e *Editor) yankLinesNoStatus(n int) {
	end := e.row + n
	if end > len(e.lines) {
		end = len(e.lines)
	}
	e.yank = nil
	for i := e.row; i < end; i++ {
		e.yank = append(e.yank, append([]rune(nil), e.lines[i]...))
	}
}

func (e *Editor) paste(below bool) {
	if e.readonly || len(e.yank) == 0 {
		return
	}
	e.pushUndo()
	at := e.row
	if below {
		at = e.row + 1
	}
	for i, l := range e.yank {
		e.lines = insertLine(e.lines, at+i, append([]rune(nil), l...))
	}
	e.row = at
	e.col = firstNonBlank(e.line())
}

func (e *Editor) doUndo() {
	if len(e.undo) == 0 {
		e.status = "already at oldest change"
		return
	}
	e.redo = append(e.redo, e.snapshot())
	s := e.undo[len(e.undo)-1]
	e.undo = e.undo[:len(e.undo)-1]
	e.restore(s)
}

func (e *Editor) doRedo() {
	if len(e.redo) == 0 {
		e.status = "already at newest change"
		return
	}
	e.undo = append(e.undo, e.snapshot())
	s := e.redo[len(e.redo)-1]
	e.redo = e.redo[:len(e.redo)-1]
	e.restore(s)
}

// --- search ----------------------------------------------------------------

func (e *Editor) findNext(forward bool) {
	if e.search == "" {
		return
	}
	n := len(e.lines)
	for step := 1; step <= n; step++ {
		r := e.row + step
		if !forward {
			r = e.row - step
		}
		r = ((r % n) + n) % n
		if idx := strings.Index(string(e.lines[r]), e.search); idx >= 0 {
			e.row = r
			e.col = len([]rune(string(e.lines[r])[:idx]))
			e.scroll()
			return
		}
	}
	// same line, after the cursor
	if idx := strings.Index(string(e.line()), e.search); idx >= 0 {
		e.col = len([]rune(string(e.line())[:idx]))
	} else {
		e.status = "pattern not found: " + e.search
	}
}

// --- scrolling & view ------------------------------------------------------

func (e *Editor) visibleRows(height int) int {
	h := height - 2 // title + status
	if h < 1 {
		h = 1
	}
	return h
}

func (e *Editor) scroll() {
	// visible height is applied at render time; keep a generous window here
	// and let View re-clamp precisely.
	if e.row < e.top {
		e.top = e.row
	}
}

// View renders the editor into width×height cells for the right panel.
func (e *Editor) View(m *Model, width, height int) []string {
	t := m.theme
	rows := e.visibleRows(height)
	if e.row < e.top {
		e.top = e.row
	}
	if e.row >= e.top+rows {
		e.top = e.row - rows + 1
	}
	if e.top < 0 {
		e.top = 0
	}

	gutter := len(fmt.Sprintf("%d", len(e.lines))) + 1
	if gutter < 3 {
		gutter = 3
	}
	textW := width - gutter - 1
	if textW < 1 {
		textW = 1
	}

	out := make([]string, height)

	// Title.
	name := e.path
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	title := " " + name
	if e.modified {
		title += " [+]"
	}
	if e.readonly {
		title += " [RO]"
	}
	out[0] = pad(t.PreviewTitle.Render(title), width)

	// Body.
	for i := 0; i < rows; i++ {
		ln := e.top + i
		if ln >= len(e.lines) {
			out[1+i] = pad(t.HiddenDim.Render(" ~"), width)
			continue
		}
		num := t.HiddenDim.Render(fmt.Sprintf("%*d ", gutter-1, ln+1))
		text := expandForDisplay(e.lines[ln])
		if ln == e.row {
			out[1+i] = num + e.renderCursorLine(t, text, textW, width, gutter)
		} else {
			out[1+i] = pad(num+truncEnd(sanitize(text), textW), width)
		}
	}

	// Status line.
	out[height-1] = e.statusLine(t, width)
	return out
}

// renderCursorLine draws the cursor row with the cursor cell highlighted.
func (e *Editor) renderCursorLine(t Theme, text string, textW, width, gutter int) string {
	runes := []rune(text)
	col := e.col
	if col > len(runes) {
		col = len(runes)
	}
	num := t.HiddenDim.Render(fmt.Sprintf("%*d ", gutter-1, e.row+1))
	// Horizontal scroll so the cursor stays visible.
	start := 0
	if col >= textW {
		start = col - textW + 1
	}
	end := start + textW
	if end > len(runes) {
		end = len(runes)
	}
	var b strings.Builder
	for i := start; i < end; i++ {
		s := sanitizeRune(runes[i])
		if i == col {
			b.WriteString(t.Cursor.Render(s))
		} else {
			b.WriteString(s)
		}
	}
	// Cursor past end of line (empty line or insert at EOL).
	if col >= len(runes) {
		b.WriteString(t.Cursor.Render(" "))
	}
	return pad(num+b.String(), width)
}

func (e *Editor) statusLine(t Theme, width int) string {
	var left string
	switch e.mode {
	case edInsert:
		left = t.StatusWarn.UnsetBackground().Render("-- INSERT --")
	case edCommand:
		left = ":" + e.cmdline
	case edSearch:
		left = "/" + e.cmdline
	default:
		if e.status != "" {
			left = t.HiddenDim.Render(e.status)
		} else if e.pending != "" {
			left = t.HiddenDim.Render("(" + e.pending + ")")
		}
	}
	right := fmt.Sprintf("%d:%d ", e.row+1, e.col+1)
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
		left = truncEnd(left, width-lipgloss.Width(right)-1)
	}
	return " " + left + strings.Repeat(" ", gap-1) + right
}

// --- small utilities -------------------------------------------------------

func insertLine(lines [][]rune, at int, l []rune) [][]rune {
	if at > len(lines) {
		at = len(lines)
	}
	lines = append(lines, nil)
	copy(lines[at+1:], lines[at:])
	lines[at] = l
	return lines
}

func removeLine(lines [][]rune, at int) [][]rune {
	if at < 0 || at >= len(lines) {
		return lines
	}
	return append(lines[:at], lines[at+1:]...)
}

func expandForDisplay(l []rune) string {
	return strings.ReplaceAll(string(l), "\t", "    ")
}

func sanitizeRune(r rune) string {
	if r == '\t' {
		return "    "
	}
	if r < 0x20 || r == 0x7f {
		return "�"
	}
	return string(r)
}
