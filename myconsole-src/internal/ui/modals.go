package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/offsideai/myconsole/internal/fsx"
	"github.com/offsideai/myconsole/internal/icloud"
)

// Modal is a centered overlay that captures all key input while open.
// Update returns the modal to keep (nil closes it).
type Modal interface {
	Update(m *Model, msg tea.KeyMsg) (Modal, tea.Cmd)
	View(m *Model, width int) string
}

// --- confirm -----------------------------------------------------------

type ConfirmModal struct {
	Title    string
	Body     []string
	YesLabel string
	Yes      func(m *Model) tea.Cmd
}

func (c *ConfirmModal) Update(m *Model, msg tea.KeyMsg) (Modal, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		return nil, c.Yes(m)
	case "n", "N", "esc", "q":
		return nil, nil
	}
	return c, nil
}

func (c *ConfirmModal) View(m *Model, width int) string {
	t := m.theme
	yes := c.YesLabel
	if yes == "" {
		yes = "confirm"
	}
	lines := []string{t.ModalTitle.Render(c.Title), ""}
	lines = append(lines, c.Body...)
	lines = append(lines, "", t.ModalDim.Render("y/enter ")+yes+t.ModalDim.Render("  ·  n/esc cancel"))
	return strings.Join(lines, "\n")
}

// --- input -------------------------------------------------------------

type InputModal struct {
	Title        string
	Input        textinput.Model
	CompletePath bool // Tab completes filesystem paths
	OnSubmit     func(m *Model, value string) tea.Cmd
}

func newInputModal(title, placeholder, initial string) *InputModal {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.SetValue(initial)
	ti.CursorEnd()
	ti.Focus()
	ti.CharLimit = 4096
	ti.Width = 44
	return &InputModal{Title: title, Input: ti}
}

func (im *InputModal) Update(m *Model, msg tea.KeyMsg) (Modal, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return nil, nil
	case "enter":
		val := strings.TrimSpace(im.Input.Value())
		if val == "" {
			return nil, nil
		}
		return nil, im.OnSubmit(m, val)
	case "tab":
		if im.CompletePath {
			im.Input.SetValue(completePath(im.Input.Value(), m.home))
			im.Input.CursorEnd()
			return im, nil
		}
	}
	var cmd tea.Cmd
	im.Input, cmd = im.Input.Update(msg)
	return im, cmd
}

func (im *InputModal) View(m *Model, width int) string {
	t := m.theme
	hint := "enter confirm · esc cancel"
	if im.CompletePath {
		hint = "tab complete · " + hint
	}
	return strings.Join([]string{
		t.ModalTitle.Render(im.Title),
		"",
		im.Input.View(),
		"",
		t.ModalDim.Render(hint),
	}, "\n")
}

// completePath extends p to the longest unambiguous existing path.
func completePath(p, home string) string {
	if p == "" {
		return p
	}
	expanded := p
	if strings.HasPrefix(p, "~") {
		expanded = home + p[1:]
	}
	dir, base := filepath.Split(expanded)
	if dir == "" {
		dir = "."
	}
	des, err := os.ReadDir(dir)
	if err != nil {
		return p
	}
	var matches []string
	for _, de := range des {
		if strings.HasPrefix(de.Name(), base) {
			name := de.Name()
			if de.IsDir() {
				name += "/"
			}
			matches = append(matches, name)
		}
	}
	if len(matches) == 0 {
		return p
	}
	sort.Strings(matches)
	common := matches[0]
	for _, s := range matches[1:] {
		for !strings.HasPrefix(s, common) {
			common = common[:len(common)-1]
		}
	}
	if common == "" {
		return p
	}
	completed := filepath.Join(dir, common)
	if strings.HasSuffix(common, "/") {
		completed += "/"
	}
	if strings.HasPrefix(p, "~") && home != "" && strings.HasPrefix(completed, home) {
		completed = "~" + completed[len(home):]
	}
	return completed
}

// --- sort --------------------------------------------------------------

type SortModal struct{ cursor int }

var sortChoices = []fsx.SortBy{fsx.SortName, fsx.SortSize, fsx.SortModified, fsx.SortKind}

func (s *SortModal) Update(m *Model, msg tea.KeyMsg) (Modal, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		return nil, nil
	case "up", "k":
		s.cursor = (s.cursor + len(sortChoices) - 1) % len(sortChoices)
	case "down", "j":
		s.cursor = (s.cursor + 1) % len(sortChoices)
	case "d":
		m.dirsFirst = !m.dirsFirst
		m.rebuildView("")
	case "enter", " ", "space":
		choice := sortChoices[s.cursor]
		if m.sortBy == choice {
			m.sortAsc = !m.sortAsc
		} else {
			m.sortBy = choice
			m.sortAsc = choice == fsx.SortName || choice == fsx.SortKind
		}
		m.rebuildView("")
	}
	return s, nil
}

func (s *SortModal) View(m *Model, width int) string {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.ModalTitle.Render("Sort by") + "\n\n")
	for i, c := range sortChoices {
		marker := "  "
		if m.sortBy == c {
			if m.sortAsc {
				marker = "↑ "
			} else {
				marker = "↓ "
			}
		}
		line := marker + c.String()
		if i == s.cursor {
			line = t.MenuItemOn.Render(pad(line, 24))
		} else {
			line = pad(line, 24)
		}
		b.WriteString(line + "\n")
	}
	df := "off"
	if m.dirsFirst {
		df = "on"
	}
	b.WriteString("\n" + t.ModalDim.Render("enter apply/flip · d folders-first: "+df+" · esc close"))
	return b.String()
}

// --- help --------------------------------------------------------------

type HelpModal struct{ scroll int }

func (h *HelpModal) lines(m *Model) []string {
	t := m.theme
	var out []string
	for _, sec := range helpSections {
		out = append(out, t.ModalTitle.Render(sec.Title))
		for _, act := range sec.Actions {
			keys := m.keys.byAction[act]
			shown := make([]string, 0, len(keys))
			for _, k := range keys {
				if k == " " {
					k = "space"
				}
				shown = append(shown, k)
			}
			out = append(out, "  "+pad(strings.Join(shown, " / "), 22)+t.ModalDim.Render(actionHelp[act]))
		}
		out = append(out, "")
	}
	return out
}

func (h *HelpModal) Update(m *Model, msg tea.KeyMsg) (Modal, tea.Cmd) {
	total := len(h.lines(m))
	page := m.listHeight() - 4
	if page < 3 {
		page = 3
	}
	switch msg.String() {
	case "esc", "q", "?", "f1", "enter":
		return nil, nil
	case "up", "k":
		h.scroll--
	case "down", "j":
		h.scroll++
	case "pgup":
		h.scroll -= page
	case "pgdown":
		h.scroll += page
	case "g":
		h.scroll = 0
	case "G":
		h.scroll = total
	}
	if h.scroll > total-page {
		h.scroll = total - page
	}
	if h.scroll < 0 {
		h.scroll = 0
	}
	return h, nil
}

func (h *HelpModal) View(m *Model, width int) string {
	lines := h.lines(m)
	page := m.listHeight() - 4
	if page < 3 {
		page = 3
	}
	end := h.scroll + page
	if end > len(lines) {
		end = len(lines)
	}
	body := strings.Join(lines[h.scroll:end], "\n")
	footer := m.theme.ModalDim.Render(fmt.Sprintf("j/k scroll (%d–%d of %d) · esc close", h.scroll+1, end, len(lines)))
	return m.theme.ModalTitle.Render("myconsole — keyboard reference") + "\n\n" + body + "\n" + footer
}

// --- info (Get Info / summaries) ----------------------------------------

type InfoModal struct {
	Title  string
	Lines  []string
	scroll int
}

func (im *InfoModal) Update(m *Model, msg tea.KeyMsg) (Modal, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "enter", "I":
		return nil, nil
	case "up", "k":
		if im.scroll > 0 {
			im.scroll--
		}
	case "down", "j":
		if im.scroll < len(im.Lines)-1 {
			im.scroll++
		}
	}
	return im, nil
}

func (im *InfoModal) View(m *Model, width int) string {
	page := m.listHeight() - 4
	if page < 3 {
		page = 3
	}
	end := im.scroll + page
	if end > len(im.Lines) {
		end = len(im.Lines)
	}
	body := strings.Join(im.Lines[im.scroll:end], "\n")
	return m.theme.ModalTitle.Render(im.Title) + "\n\n" + body + "\n\n" + m.theme.ModalDim.Render("esc close")
}

func newSummaryModal(msg summaryMsg) *InfoModal {
	total := msg.sum.LocalBytes + msg.sum.CloudBytes
	lines := []string{
		fmt.Sprintf("local files:      %6d   %s", msg.sum.LocalFiles, humanBytes(msg.sum.LocalBytes)),
		fmt.Sprintf("cloud-only files: %6d   %s", msg.sum.CloudFiles, humanBytes(msg.sum.CloudBytes)),
		fmt.Sprintf("total:            %6d   %s", msg.sum.LocalFiles+msg.sum.CloudFiles, humanBytes(total)),
	}
	if msg.capped {
		lines = append(lines, "", "(scan capped — folder is very large)")
	}
	return &InfoModal{Title: "iCloud summary — " + filepath.Base(msg.root), Lines: lines}
}

// --- collision (paste conflicts) -----------------------------------------

type pasteMode int

const (
	pasteReplace pasteMode = iota
	pasteKeepBoth
	pasteSkip
)

type CollisionModal struct {
	conflicts []string
	cursor    int
}

var collisionChoices = []struct {
	label string
	mode  pasteMode
}{
	{"Keep both (rename incoming)", pasteKeepBoth},
	{"Replace existing", pasteReplace},
	{"Skip conflicting items", pasteSkip},
}

func (c *CollisionModal) Update(m *Model, msg tea.KeyMsg) (Modal, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		return nil, nil
	case "up", "k":
		c.cursor = (c.cursor + len(collisionChoices) - 1) % len(collisionChoices)
	case "down", "j":
		c.cursor = (c.cursor + 1) % len(collisionChoices)
	case "enter", " ", "space":
		return nil, m.runPaste(collisionChoices[c.cursor].mode)
	}
	return c, nil
}

func (c *CollisionModal) View(m *Model, width int) string {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.ModalTitle.Render(fmt.Sprintf("%d name conflict(s)", len(c.conflicts))) + "\n\n")
	show := c.conflicts
	if len(show) > 5 {
		show = show[:5]
	}
	for _, n := range show {
		b.WriteString(t.ModalDim.Render("  • ") + truncEnd(sanitize(n), 48) + "\n")
	}
	if len(c.conflicts) > 5 {
		b.WriteString(t.ModalDim.Render(fmt.Sprintf("  … and %d more", len(c.conflicts)-5)) + "\n")
	}
	b.WriteString("\n")
	for i, ch := range collisionChoices {
		line := "  " + ch.label
		if i == c.cursor {
			line = t.MenuItemOn.Render(pad(line, 36))
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + t.ModalDim.Render("enter choose · esc cancel paste"))
	return b.String()
}

// --- download queue manager ----------------------------------------------

type QueueModal struct{ cursor int }

func (qm *QueueModal) Update(m *Model, msg tea.KeyMsg) (Modal, tea.Cmd) {
	n := len(m.qsnap.Items)
	clamp := func() {
		if qm.cursor >= n {
			qm.cursor = n - 1
		}
		if qm.cursor < 0 {
			qm.cursor = 0
		}
	}
	switch msg.String() {
	case "esc", "q", "Q":
		return nil, nil
	case "up", "k":
		qm.cursor--
	case "down", "j":
		qm.cursor++
	case "K", "+":
		m.queue.Move(qm.cursor, -1)
		qm.cursor--
	case "J", "-":
		m.queue.Move(qm.cursor, +1)
		qm.cursor++
	case "c":
		m.queue.Cancel(qm.cursor)
	case "C":
		m.queue.CancelAll()
	case "p":
		m.setQsnap(m.queue.Snapshot())
		m.queue.SetPaused(!m.qsnap.Paused)
	case "x":
		m.queue.ClearFinished()
	}
	m.setQsnap(m.queue.Snapshot())
	n = len(m.qsnap.Items)
	clamp()
	var cmd tea.Cmd
	if !m.ticking && m.queue.HasActive() {
		m.ticking = true
		cmd = m.queueTickCmd()
	}
	return qm, cmd
}

func (qm *QueueModal) View(m *Model, width int) string {
	t := m.theme
	var b strings.Builder
	title := "Download queue"
	if m.qsnap.Paused {
		title += "  (paused)"
	}
	b.WriteString(t.ModalTitle.Render(title) + "\n\n")
	if len(m.qsnap.Items) == 0 {
		b.WriteString(t.ModalDim.Render("empty — mark files with d to download") + "\n")
	}
	page := m.listHeight() - 6
	if page < 3 {
		page = 3
	}
	start := 0
	if qm.cursor >= page {
		start = qm.cursor - page + 1
	}
	for i := start; i < len(m.qsnap.Items) && i < start+page; i++ {
		it := m.qsnap.Items[i]
		pct := ""
		if it.Size > 0 && (it.State == icloud.StateDownloading || it.State == icloud.StateStalled) {
			pct = fmt.Sprintf(" %3.0f%%", 100*float64(it.Got)/float64(it.Size))
		}
		var stateStyle = t.ModalDim
		switch it.State {
		case icloud.StateDownloading, icloud.StateStarting:
			stateStyle = t.GlyphActive
		case icloud.StateDone:
			stateStyle = t.GlyphLocal
		case icloud.StateFailed, icloud.StateStalled:
			stateStyle = t.StatusErr.UnsetBackground()
		}
		line := pad(truncMiddle(sanitize(filepath.Base(it.Path)), 34), 35) +
			padLeft(humanBytes(it.Size), 10) + "  " +
			stateStyle.Render(pad(it.State.String()+pct, 16))
		if i == qm.cursor {
			line = t.Cursor.Render(line)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + t.ModalDim.Render("c cancel · C cancel all · p pause · K/J reorder · x clear finished · esc close"))
	return b.String()
}

// --- fuzzy find ----------------------------------------------------------

type FindModal struct {
	input     textinput.Model
	results   []fsx.FindResult
	cursor    int
	searching bool
	searched  bool
	capped    bool
	inResults bool
}

func newFindModal() *FindModal {
	ti := textinput.New()
	ti.Placeholder = "fuzzy query"
	ti.Prompt = "find: "
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 44
	return &FindModal{input: ti}
}

func (f *FindModal) absorb(msg findResultsMsg) {
	if msg.query != strings.TrimSpace(f.input.Value()) {
		return // stale search
	}
	f.searching = false
	f.searched = true
	f.results = msg.results
	f.capped = msg.capped
	f.cursor = 0
	f.inResults = len(f.results) > 0
}

func findCmd(root, query string, includeHidden bool) tea.Cmd {
	return func() tea.Msg {
		results, capped := fsx.FuzzyFind(root, query, 50000, 200, includeHidden)
		return findResultsMsg{query: query, results: results, capped: capped}
	}
}

func (f *FindModal) Update(m *Model, msg tea.KeyMsg) (Modal, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if f.inResults {
			f.inResults = false
			return f, nil
		}
		return nil, nil
	case "enter":
		if f.inResults && f.cursor < len(f.results) {
			r := f.results[f.cursor]
			dir := filepath.Dir(r.Path)
			keep := r.Path
			if r.IsDir {
				dir, keep = r.Path, ""
			}
			return nil, m.navigateTo(dir, keep)
		}
		q := strings.TrimSpace(f.input.Value())
		if q == "" {
			return f, nil
		}
		f.searching = true
		f.searched = false
		return f, findCmd(m.cwd, q, m.showHidden)
	case "down", "ctrl+n":
		if len(f.results) > 0 {
			if f.inResults {
				f.cursor = (f.cursor + 1) % len(f.results)
			}
			f.inResults = true
		}
		return f, nil
	case "up", "ctrl+p":
		if f.inResults && len(f.results) > 0 {
			f.cursor = (f.cursor + len(f.results) - 1) % len(f.results)
		}
		return f, nil
	}
	f.inResults = false
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	return f, cmd
}

func (f *FindModal) View(m *Model, width int) string {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.ModalTitle.Render("Fuzzy find under "+abbreviateHome(m.cwd, m.home)) + "\n\n")
	b.WriteString(f.input.View() + "\n\n")
	switch {
	case f.searching:
		b.WriteString(t.ModalDim.Render("searching…") + "\n")
	case f.searched && len(f.results) == 0:
		b.WriteString(t.ModalDim.Render("no matches") + "\n")
	}
	page := m.listHeight() - 7
	if page < 3 {
		page = 3
	}
	start := 0
	if f.cursor >= page {
		start = f.cursor - page + 1
	}
	for i := start; i < len(f.results) && i < start+page; i++ {
		r := f.results[i]
		icon := "  "
		if r.IsDir {
			icon = "▸ "
		}
		line := icon + truncMiddle(sanitize(r.Rel), 52)
		if i == f.cursor && f.inResults {
			line = t.Cursor.Render(pad(line, 56))
		}
		b.WriteString(line + "\n")
	}
	hints := "enter search · esc close"
	if len(f.results) > 0 {
		hints = "↓/↑ pick · enter open · type to edit query · esc close"
		if f.capped {
			hints += " · (scan capped)"
		}
	}
	b.WriteString("\n" + t.ModalDim.Render(hints))
	return b.String()
}
