package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/offsideai/mydb/internal/dbx"
	"github.com/offsideai/mydb/internal/registry"
)

// sqlSession is one connection's SQL tab: a persistent editor buffer
// above a results pane. Sessions survive tab and tree switches.
type sqlSession struct {
	connID int64
	editor *Editor
	gv     gridView

	running bool
	qid     int // matches queryDoneMsg to the run that produced it
	cancel  context.CancelFunc
	lastSQL string

	res       *dbx.Result
	errText   string
	elapsed   time.Duration
	cancelled bool

	editorFocused bool // editor vs results focus within the tab
}

type queryDoneMsg struct {
	connID  int64
	qid     int
	sql     string
	started time.Time
	res     *dbx.Result
	err     error
}

// currentSQLSession returns (creating on demand) the session for the
// cursor's connection; nil when the cursor is outside any connection.
func (m *Model) currentSQLSession(create bool) *sqlSession {
	c := m.cursorConn()
	if c == nil {
		return nil
	}
	if s, ok := m.sqlSessions[c.ID]; ok {
		return s
	}
	if !create {
		return nil
	}
	s := &sqlSession{
		connID:        c.ID,
		editor:        newEditor("SQL — "+c.Name, nil, false),
		editorFocused: true,
	}
	m.sqlSessions[c.ID] = s
	return s
}

// editorSubmit is the editor's :w hook — run the buffer.
func editorSubmit(m *Model, e *Editor) tea.Cmd {
	if m == nil {
		return nil
	}
	for _, s := range m.sqlSessions {
		if s.editor == e {
			return m.runSession(s, strings.TrimSpace(string(e.Content())))
		}
	}
	return nil
}

// runSession executes sql on the session's connection; a run already in
// flight asks to cancel-and-run first.
func (m *Model) runSession(s *sqlSession, sql string) tea.Cmd {
	if sql == "" {
		return m.note(levelInfo, "nothing to run")
	}
	conn := m.open[s.connID]
	if conn == nil {
		return m.note(levelWarn, "not connected — press "+m.keys.KeyFor(ActConnect)+" on the connection")
	}
	if s.running {
		prev := s.cancel
		m.modal = &ConfirmModal{
			Title:    "A query is already running",
			Body:     []string{"Cancel it and run the new one?"},
			YesLabel: "cancel and run",
			Yes: func(m *Model) tea.Cmd {
				if prev != nil {
					prev()
				}
				s.running = false
				return m.runSession(s, sql)
			},
		}
		return nil
	}
	m.qSeq++
	qid := m.qSeq
	ctx, cancel := context.WithCancel(context.Background())
	s.running = true
	s.qid = qid
	s.cancel = cancel
	s.lastSQL = sql
	s.cancelled = false
	started := time.Now()
	maxRows := m.cfg.Query.MaxRows
	connID := s.connID
	return tea.Batch(m.ensureSpin(), func() tea.Msg {
		res, err := conn.Query(ctx, "", sql, dbx.QueryReq{MaxRows: maxRows})
		cancel()
		return queryDoneMsg{connID: connID, qid: qid, sql: sql, started: started, res: res, err: err}
	})
}

// cancelSession stops the in-flight query, if any.
func (m *Model) cancelSession(s *sqlSession) bool {
	if s == nil || !s.running || s.cancel == nil {
		return false
	}
	s.cancelled = true
	s.cancel()
	return true
}

// absorbQueryDone lands a finished run into its session and records it.
func (m *Model) absorbQueryDone(msg queryDoneMsg) tea.Cmd {
	elapsed := time.Since(msg.started)
	s := m.sqlSessions[msg.connID]
	if s != nil && s.qid == msg.qid {
		s.running = false
		s.cancel = nil
		s.elapsed = elapsed
		if msg.err != nil {
			s.res = nil
			s.gv.setResult(nil)
			if s.cancelled || errors.Is(msg.err, context.Canceled) {
				s.errText = "cancelled"
			} else {
				s.errText = msg.err.Error()
			}
		} else {
			s.errText = ""
			s.res = msg.res
			s.gv = gridView{}
			s.gv.setResult(msg.res)
		}
	}
	// History records every finished run, stale or not.
	entry := registry.HistoryEntry{
		ConnectionID: msg.connID,
		SQL:          msg.sql,
		StartedAt:    msg.started.UTC().Format(time.RFC3339),
		DurationMs:   elapsed.Milliseconds(),
		OK:           msg.err == nil,
	}
	if msg.err != nil {
		entry.Error = msg.err.Error()
	} else if msg.res != nil {
		entry.Rows = int64(len(msg.res.Rows))
	}
	limit := m.cfg.Query.HistoryLimit
	return regCmd("", func() error { return m.reg.AddHistory(entry, limit) })
}

// sqlTabKey routes keys while the SQL tab is focused.
func (m *Model) sqlTabKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	s := m.currentSQLSession(true)
	if s == nil {
		switch key {
		case "tab", "esc":
			m.focusRight = false
		case "[":
			return m.switchTab(-1)
		case "]":
			return m.switchTab(+1)
		}
		return nil
	}

	// Run works from anywhere in the tab, any editor mode.
	if key == "ctrl+r" {
		return m.runSession(s, strings.TrimSpace(string(s.editor.Content())))
	}

	if s.editorFocused {
		if key == "tab" && s.editor.mode == edNormal {
			s.editorFocused = false
			return nil
		}
		cmd := s.editor.Update(m, msg)
		if s.editor.quit {
			s.editor.quit = false
			s.editorFocused = false
			m.focusRight = false
		}
		return cmd
	}

	// Results pane focus.
	switch key {
	case "esc":
		if m.cancelSession(s) {
			return m.note(levelWarn, "cancelling…")
		}
		s.editorFocused = true
		return nil
	case "tab":
		m.focusRight = false
		s.editorFocused = true // next visit starts in the editor
		return nil
	case "i":
		s.editorFocused = true
		return nil
	case "[":
		return m.switchTab(-1)
	case "]":
		return m.switchTab(+1)
	case "e":
		return m.runExplain(s, false)
	case "E":
		return m.runExplain(s, true)
	case "ctrl+h":
		return m.openHistory()
	}
	if cmd, handled := m.gridNavKey(&s.gv, "result", key); handled {
		return cmd
	}
	return nil
}

// runExplain wraps the buffer in the engine's EXPLAIN form. The ANALYZE
// variant executes the statement, so it confirms first.
func (m *Model) runExplain(s *sqlSession, analyze bool) tea.Cmd {
	caps := m.capsFor(s.connID)
	sql := strings.TrimSpace(string(s.editor.Content()))
	if sql == "" {
		return m.note(levelInfo, "nothing to explain")
	}
	if !analyze {
		if caps.Explain == "" {
			return m.note(levelWarn, "engine has no EXPLAIN")
		}
		return m.runSession(s, caps.Explain+sql)
	}
	if caps.ExplainAnalyze == "" {
		return m.note(levelWarn, "engine has no EXPLAIN ANALYZE")
	}
	m.modal = &ConfirmModal{
		Title:    "EXPLAIN ANALYZE executes the statement",
		Body:     []string{"Writes in the buffer WILL run. Continue?"},
		YesLabel: "run it",
		Yes: func(m *Model) tea.Cmd {
			return m.runSession(s, caps.ExplainAnalyze+sql)
		},
	}
	return nil
}

// --- rendering -------------------------------------------------------------

// renderSQLTab draws the editor over the results pane.
func (m *Model) renderSQLTab(width, height int) []string {
	t := m.theme
	s := m.currentSQLSession(false)
	if s == nil {
		out := make([]string, height)
		for i := range out {
			line := ""
			if i == 1 {
				line = " " + t.Dim.Render("select a connection, then write SQL here")
			}
			if gap := width - lipgloss.Width(line); gap > 0 {
				line += strings.Repeat(" ", gap)
			}
			out[i] = line
		}
		return out
	}

	editorH := (height - 1) * 55 / 100
	if editorH < 4 {
		editorH = 4
	}
	resultsH := height - editorH - 1
	if resultsH < 1 {
		resultsH = 1
	}

	lines := s.editor.View(m, width, editorH)

	sepLabel := " results "
	if s.editorFocused {
		sepLabel = " results (tab focuses) "
	}
	sep := t.Dim.Render("─" + sepLabel + strings.Repeat("─", max0(width-lipgloss.Width(sepLabel)-2)))
	if gap := width - lipgloss.Width(sep); gap > 0 {
		sep += strings.Repeat(" ", gap)
	}
	lines = append(lines, sep)
	lines = append(lines, m.renderSQLResults(s, width, resultsH)...)
	return lines[:height]
}

func (m *Model) renderSQLResults(s *sqlSession, width, height int) []string {
	t := m.theme
	blank := func(str string) string {
		if gap := width - lipgloss.Width(str); gap > 0 {
			str += strings.Repeat(" ", gap)
		}
		return str
	}
	var out []string
	switch {
	case s.running:
		out = append(out, blank(" "+t.StatusWarn.UnsetBackground().Render(
			spinnerFrames[m.tickN%len(spinnerFrames)]+" running…")+
			t.Dim.Render("  (esc on results cancels)")))
	case s.errText != "":
		for _, l := range wrapText(s.errText, width-3) {
			out = append(out, blank(" "+t.Danger.Render(l)))
			if len(out) >= height-1 {
				break
			}
		}
		out = append(out, blank(" "+t.Dim.Render(fmt.Sprintf("%.1f ms", ms(s.elapsed)))))
	case s.res != nil:
		body := height - 1
		if len(s.res.Rows) > 0 {
			out = m.gridRows(&s.gv, width, body, m.focusRight && !s.editorFocused)
		}
		foot := fmt.Sprintf(" %d row(s) · %d stmt(s) · %d affected · %.1f ms",
			len(s.res.Rows), s.res.Stmts, s.res.Affected, ms(s.res.Elapsed))
		if s.res.Truncated {
			foot += " · capped at max_rows"
		}
		out = append(out, blank(t.Dim.Render(truncEnd(foot, width))))
	default:
		out = append(out, blank(" "+t.Dim.Render("ctrl+r or :w runs the buffer · e explains (results focus)")))
	}
	for len(out) < height {
		out = append(out, strings.Repeat(" ", width))
	}
	return out[:height]
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// wrapText hard-wraps s to width for error display.
func wrapText(s string, width int) []string {
	if width < 8 {
		width = 8
	}
	var out []string
	for _, part := range strings.Split(s, "\n") {
		part = sanitize(part)
		for len(part) > width {
			out = append(out, part[:width])
			part = part[width:]
		}
		out = append(out, part)
	}
	return out
}
