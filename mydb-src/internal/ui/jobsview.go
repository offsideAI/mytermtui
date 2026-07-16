package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/offsideai/mydb/internal/config"
	"github.com/offsideai/mydb/internal/jobs"
	"github.com/offsideai/mydb/internal/registry"
)

// Jobs poll cadence while anything is active.
const jobPollMs = 400

type jobsTickMsg struct{}

func (m *Model) jobsTickCmd() tea.Cmd {
	return tea.Tick(jobPollMs*time.Millisecond, func(time.Time) tea.Msg { return jobsTickMsg{} })
}

// onJobDone logs a finished job to jobs_history (called from Tick on the
// UI goroutine).
func (m *Model) onJobDone(d jobs.Completion) {
	j := d.Job
	ok := j.State == jobs.Done
	detail := j.Phase
	if j.Err != "" {
		detail = j.Err
	}
	_ = m.reg.AddJob(registry.JobEntry{
		ConnectionID: j.ConnID, Kind: j.Kind.String(), Target: j.Dest,
		StartedAt:  j.Started.UTC().Format(time.RFC3339),
		FinishedAt: j.Finished.UTC().Format(time.RFC3339),
		OK:         ok, Detail: detail,
	})
	m.jobNote = jobNote(m, j)
}

func jobNote(m *Model, j jobs.Job) tea.Cmd {
	switch j.State {
	case jobs.Done:
		return m.note(levelOK, j.Kind.String()+" done: "+filepath.Base(j.Dest))
	case jobs.Failed:
		return m.note(levelErr, j.Kind.String()+" failed: "+j.Err)
	case jobs.Cancelled:
		return m.note(levelWarn, j.Kind.String()+" cancelled")
	}
	return nil
}

// startJobsIfIdle kicks the poll loop when the first job is added.
func (m *Model) ensureJobsTick() tea.Cmd {
	if m.jobsTicking || !m.jobsQ.HasActive() {
		return nil
	}
	m.jobsTicking = true
	return m.jobsTickCmd()
}

// --- actions ----------------------------------------------------------------

// backupTarget opens the destination-path dialog for the cursor
// connection, then enqueues the appropriate backup job.
func (m *Model) startBackup() tea.Cmd {
	c := m.cursorConn()
	if c == nil {
		return m.note(levelWarn, "select a connection first")
	}
	if m.open[c.ID] == nil && c.Engine == "sqlite" {
		// SQLite backup opens its own read-only handle; no live conn needed.
	} else if m.open[c.ID] == nil {
		return m.note(levelWarn, "connect first")
	}
	def := m.defaultBackupPath(*c)
	m.modal = newInputModal("Back up "+c.Name+" to:", def, def, func(m *Model, dest string) tea.Cmd {
		return m.enqueueBackup(*c, config.ExpandTilde(dest))
	})
	return nil
}

func (m *Model) defaultBackupPath(c registryConn) string {
	dir := config.ExpandTilde(m.cfg.Backup.Dir)
	base := c.Name
	ext := ".dump"
	if c.Engine == "sqlite" {
		ext = ".db"
	}
	return filepath.Join(dir, base+ext)
}

func (m *Model) enqueueBackup(c registryConn, dest string) tea.Cmd {
	if err := ensureDir(dest); err != nil {
		return m.note(levelErr, err.Error())
	}
	label := "backup " + c.Name + " → " + filepath.Base(dest)
	var run jobs.Run
	switch c.Engine {
	case "sqlite":
		if err := jobs.SQLiteIntegrityCheck(config.ExpandTilde(c.Path)); err != nil {
			return m.note(levelErr, err.Error())
		}
		run = jobs.SQLiteBackup(config.ExpandTilde(c.Path), dest)
	case "postgres":
		run = m.pgBackupRun(c, dest)
		if run == nil {
			return nil // note already emitted
		}
	default:
		return m.note(levelWarn, "backup not supported for "+c.Engine)
	}
	m.jobsQ.Add(jobs.KindBackup, c.ID, label, dest, run)
	return tea.Batch(m.note(levelInfo, "queued "+label), m.ensureJobsTick())
}

// pgBackupRun picks pg_dump or the plain-SQL fallback.
func (m *Model) pgBackupRun(c registryConn, dest string) jobs.Run {
	pc := jobs.PGConn{Host: c.Host, Port: c.Port, DBName: c.DBName, Username: c.Username, Password: c.Secret}
	_, haveDump := jobs.Tool("pg_dump")
	if m.cfg.Backup.PreferPgDump && haveDump {
		return jobs.PGDumpBackup(pc, dest)
	}
	// Fallback: needs a live connection to read the data.
	conn := m.open[c.ID]
	if conn == nil {
		m.note(levelWarn, "connect first (pg_dump not found; using the data-only fallback)")
		return nil
	}
	if !haveDump {
		m.note(levelWarn, "pg_dump not on PATH — writing a data-only fallback dump")
	}
	return jobs.PlainSQLDump(conn, c.DBName, []string{"public"}, dest)
}

// startRestore opens a source-path dialog and enqueues a restore.
func (m *Model) startRestore() tea.Cmd {
	c := m.cursorConn()
	if c == nil {
		return m.note(levelWarn, "select a connection first")
	}
	if c.Engine != "postgres" {
		return m.note(levelInfo, "SQLite restore: add the backup file as a new connection (B)")
	}
	if m.open[c.ID] == nil {
		return m.note(levelWarn, "connect first")
	}
	def := m.defaultBackupPath(*c)
	m.modal = newInputModal("Restore "+c.Name+" from:", def, def, func(m *Model, src string) tea.Cmd {
		pc := jobs.PGConn{Host: c.Host, Port: c.Port, DBName: c.DBName, Username: c.Username, Password: c.Secret}
		run := jobs.PGRestore(pc, config.ExpandTilde(src))
		label := "restore " + c.Name + " ← " + filepath.Base(src)
		m.jobsQ.Add(jobs.KindRestore, c.ID, label, config.ExpandTilde(src), run)
		return tea.Batch(m.note(levelInfo, "queued "+label), m.ensureJobsTick())
	})
	return nil
}

// --- Jobs tab + job bar rendering -------------------------------------------

func (m *Model) renderJobsTab(width, height int) []string {
	t := m.theme
	snap := m.jobsQ.Snapshot()
	out := make([]string, 0, height)
	blank := func(s string) string {
		if gap := width - lipgloss.Width(s); gap > 0 {
			s += strings.Repeat(" ", gap)
		}
		return s
	}
	if len(snap.Jobs) == 0 {
		out = append(out, blank(" "+t.Dim.Render("no jobs — b backs up the selected connection")))
	}
	for i := len(snap.Jobs) - 1; i >= 0 && len(out) < height-1; i-- {
		j := snap.Jobs[i]
		out = append(out, blank(" "+m.renderJobLine(j, width-2)))
	}
	for len(out) < height {
		out = append(out, strings.Repeat(" ", width))
	}
	return out[:height]
}

func (m *Model) renderJobLine(j jobs.Job, width int) string {
	t := m.theme
	var state string
	switch j.State {
	case jobs.Running:
		state = t.StatusWarn.UnsetBackground().Render(spinnerFrames[m.tickN%len(spinnerFrames)] + " " + j.Phase)
	case jobs.Done:
		state = t.GlyphOpen.Render("✓ done")
	case jobs.Failed:
		state = t.GlyphFailed.Render("✗ " + j.Err)
	case jobs.Cancelled:
		state = t.Dim.Render("cancelled")
	default:
		state = t.Dim.Render("queued")
	}
	size := ""
	if j.BytesDone > 0 {
		size = " · " + humanBytes(j.BytesDone)
		if j.BytesTot > 0 {
			size = fmt.Sprintf(" · %.0f%%", 100*float64(j.BytesDone)/float64(j.BytesTot))
		}
	}
	line := t.PanelMeta.Render(fmt.Sprintf("#%d ", j.ID)) + truncEnd(sanitize(j.Label), 40) + size + "  " + state
	return truncEnd(line, width)
}

// jobBarVisible reports whether the transient job bar should show.
func (m *Model) jobBarVisible() bool { return m.jobsQ.Snapshot().ActiveN > 0 }

func (m *Model) renderJobBar() string {
	t := m.theme
	snap := m.jobsQ.Snapshot()
	label := fmt.Sprintf(" %s %d running · %d queued", spinnerFrames[m.tickN%len(spinnerFrames)], snap.RunningN, snap.QueuedN)
	// Show the current job's phase + bytes.
	var cur string
	for i := len(snap.Jobs) - 1; i >= 0; i-- {
		if snap.Jobs[i].State == jobs.Running {
			j := snap.Jobs[i]
			cur = "  " + truncEnd(sanitize(j.Label), 30)
			if j.Phase != "" {
				cur += " · " + truncEnd(sanitize(j.Phase), 28)
			}
			if j.BytesDone > 0 {
				cur += " · " + humanBytes(j.BytesDone)
			}
			break
		}
	}
	line := t.StatusWarn.Render(label) + t.Dim.Render(cur)
	if gap := m.w - lipgloss.Width(line); gap > 0 {
		line += strings.Repeat(" ", gap)
	}
	return line
}

// jobsKey handles keys while the Jobs tab is focused.
func (m *Model) jobsKey(key string) tea.Cmd {
	snap := m.jobsQ.Snapshot()
	switch key {
	case "up", "k":
		if m.jobCursor > 0 {
			m.jobCursor--
		}
	case "down", "j":
		if m.jobCursor < len(snap.Jobs)-1 {
			m.jobCursor++
		}
	case "c":
		if id := jobIDAt(snap, m.jobCursor); id > 0 {
			m.jobsQ.Cancel(id)
			return m.ensureJobsTick()
		}
	case "x":
		m.jobsQ.ClearFinished()
	}
	return nil
}

// jobIDAt maps the display cursor (newest-first) to a job ID.
func jobIDAt(snap jobs.Snapshot, cursor int) int {
	idx := len(snap.Jobs) - 1 - cursor
	if idx < 0 || idx >= len(snap.Jobs) {
		return 0
	}
	return snap.Jobs[idx].ID
}

// --- small helpers ----------------------------------------------------------

func ensureDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}

// newInputModal builds a prompt dialog with a submit callback.
func newInputModal(title, placeholder, initial string, onSubmit func(m *Model, value string) tea.Cmd) *InputModal {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.SetValue(initial)
	ti.CursorEnd()
	ti.Focus()
	ti.CharLimit = 1024
	ti.Width = 50
	return &InputModal{Title: title, Input: ti, OnSubmit: onSubmit}
}
