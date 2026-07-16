package ui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/offsideai/mydb/internal/dbx"
)

// The admin path (§3.6, §4.6): every UI-generated write is a dbx.Plan
// that flows through runPlan — read-only check, danger-tier
// confirmation showing the exact SQL, then execution.

// adminTimeout bounds plan execution; maintenance (VACUUM on a big
// database) legitimately runs far longer than introspection.
const adminTimeout = 10 * time.Minute

type adminDoneMsg struct {
	connID int64
	title  string
	res    *dbx.Result
	err    error
}

// runPlan is the choke point. It may install a confirmation modal; the
// caller must not assume the plan ran synchronously.
func (m *Model) runPlan(connID int64, p dbx.Plan) tea.Cmd {
	c, ok := m.connByID[connID]
	if !ok {
		return nil
	}
	if c.ReadOnly() {
		return m.note(levelWarn, c.Name+" is read-only (⏸) — writes are disabled")
	}
	if m.open[connID] == nil {
		return m.note(levelWarn, "not connected — press "+m.keys.KeyFor(ActConnect)+" first")
	}
	exec := func(m *Model) tea.Cmd { return m.execPlan(connID, p) }
	switch p.Danger {
	case dbx.DangerHigh:
		m.modal = newTypedConfirm(p.Title, planBody(m, p), p.ConfirmToken, exec)
		return nil
	case dbx.DangerMedium:
		if !m.cfg.General.ConfirmMedium {
			return exec(m)
		}
		m.modal = &ConfirmModal{Title: p.Title, Body: planBody(m, p), YesLabel: "run it", Yes: exec}
		return nil
	default:
		return exec(m)
	}
}

// planBody renders the confirmation dialog: blast radius + exact SQL.
func planBody(m *Model, p dbx.Plan) []string {
	t := m.theme
	out := []string{p.Summary, ""}
	for _, s := range p.Stmts {
		out = append(out, t.Table.Render(truncEnd(sanitize(s), 70)+";"))
	}
	return out
}

func (m *Model) execPlan(connID int64, p dbx.Plan) tea.Cmd {
	conn := m.open[connID]
	if conn == nil {
		return m.note(levelWarn, "not connected")
	}
	c := m.connByID[connID]
	// Postgres' simple protocol already runs multi-statement scripts in
	// one implicit transaction; SQLite executes statement-by-statement
	// and needs the explicit wrap.
	script := p.Script(c.Engine == "sqlite" && m.capsFor(connID).TransactionalDDL)
	title := p.Title
	db := p.DB
	return tea.Batch(m.note(levelInfo, title+"…"), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), adminTimeout)
		defer cancel()
		res, err := conn.Query(ctx, db, script, dbx.QueryReq{MaxRows: 1000})
		return adminDoneMsg{connID: connID, title: title, res: res, err: err}
	})
}

// absorbAdminDone lands a finished plan: note, show read-back rows
// (integrity checks, EXPLAIN-style output), refresh derived state.
func (m *Model) absorbAdminDone(msg adminDoneMsg) tea.Cmd {
	if msg.err != nil {
		return m.note(levelErr, msg.title+": "+msg.err.Error())
	}
	cmds := []tea.Cmd{m.note(levelOK, msg.title+" — done")}
	if msg.res != nil && len(msg.res.Rows) > 0 {
		m.modal = &InfoModal{Title: msg.title, Lines: resultLines(msg.res)}
	}
	cmds = append(cmds, m.refreshConn(msg.connID))
	return tea.Batch(cmds...)
}

// resultLines flattens a small result set for the InfoModal.
func resultLines(res *dbx.Result) []string {
	var out []string
	for _, row := range res.Rows {
		parts := make([]string, len(row))
		for i, v := range row {
			parts[i] = sanitize(v.S)
		}
		out = append(out, truncEnd(strings.Join(parts, "  "), 76))
	}
	if res.Truncated {
		out = append(out, "… (truncated)")
	}
	return out
}

// refreshConn refetches one connection's derived state after a
// successful admin operation: schema children, roles, discovered
// databases.
func (m *Model) refreshConn(id int64) tea.Cmd {
	conn := m.open[id]
	if conn == nil {
		return nil
	}
	c := m.connByID[id]
	caps := m.capsFor(id)
	var cmds []tea.Cmd
	path := dbx.ConnPath(c.Locality, id)
	if m.expanded[path] {
		cmds = append(cmds, fetchChildrenCmd(conn, caps, connNode(c)))
	}
	delete(m.childCache, rolesServerPath(id))
	if caps.MultipleDatabases {
		cmds = append(cmds, discoverCmd(conn, id))
	}
	return tea.Batch(cmds...)
}

// --- template picker + parameter flow (§3.5) --------------------------------

// TemplatePicker lists the engine's common commands.
type TemplatePicker struct {
	connID    int64
	templates []dbx.Template
	cursor    int
}

func (m *Model) openCommands() tea.Cmd {
	c := m.cursorConn()
	if c == nil {
		return m.note(levelWarn, "select a connection first")
	}
	tpls := dbx.TemplatesFor(c.Engine)
	if len(tpls) == 0 {
		return m.note(levelInfo, "no command templates for "+c.Engine+" — try "+m.keys.KeyFor(ActMaintenance)+" for maintenance")
	}
	m.modal = &TemplatePicker{connID: c.ID, templates: tpls}
	return nil
}

func (tp *TemplatePicker) Update(m *Model, msg tea.KeyMsg) (Modal, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		return nil, nil
	case "up", "k":
		if tp.cursor > 0 {
			tp.cursor--
		}
	case "down", "j":
		if tp.cursor < len(tp.templates)-1 {
			tp.cursor++
		}
	case "enter":
		return newTemplateFlow(tp.connID, tp.templates[tp.cursor]), nil
	}
	return tp, nil
}

func (tp *TemplatePicker) View(m *Model, width int) string {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.ModalTitle.Render("Common commands") + "\n\n")
	for i, tpl := range tp.templates {
		line := "  " + tpl.Label
		if tpl.Danger == dbx.DangerHigh {
			line = "  " + t.Danger.Render(tpl.Label)
		}
		if i == tp.cursor {
			line = t.MenuItemOn.Render(pad("  "+tpl.Label, 40))
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + t.ModalDim.Render("enter choose · esc close"))
	return b.String()
}

// templateFlow collects the template's parameters one dialog at a time,
// previews the exact SQL, then hands the plan to runPlan.
type templateFlow struct {
	connID  int64
	tpl     dbx.Template
	values  map[string]string
	step    int
	input   textinput.Model
	preview *dbx.Plan
	errMsg  string
}

func newTemplateFlow(connID int64, tpl dbx.Template) *templateFlow {
	f := &templateFlow{connID: connID, tpl: tpl, values: map[string]string{}}
	f.resetInput()
	return f
}

func (f *templateFlow) resetInput() {
	ti := textinput.New()
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 44
	if f.step < len(f.tpl.Params) && f.tpl.Params[f.step].Kind == dbx.ParamSecret {
		ti.EchoMode = textinput.EchoPassword
	}
	f.input = ti
}

func (f *templateFlow) Update(m *Model, msg tea.KeyMsg) (Modal, tea.Cmd) {
	key := msg.String()
	if key == "esc" {
		return nil, nil
	}

	if f.preview != nil {
		if key == "enter" {
			// runPlan may install the confirmation modal; return whatever
			// it decided (nil when the plan ran without ceremony).
			m.modal = nil
			cmd := m.runPlan(f.connID, *f.preview)
			return m.modal, cmd
		}
		return f, nil
	}

	switch key {
	case "enter":
		val := strings.TrimSpace(f.input.Value())
		p := f.tpl.Params[f.step]
		if val == "" || (p.Kind == dbx.ParamIdent && !dbx.ValidIdent(val)) {
			f.errMsg = p.Label + " is required"
			return f, nil
		}
		f.errMsg = ""
		f.values[p.Name] = val
		f.step++
		if f.step >= len(f.tpl.Params) {
			plan := f.tpl.Build(f.values)
			f.preview = &plan
			return f, nil
		}
		f.resetInput()
		return f, nil
	case "ctrl+r":
		if f.step < len(f.tpl.Params) && f.tpl.Params[f.step].Kind == dbx.ParamSecret {
			if f.input.EchoMode == textinput.EchoPassword {
				f.input.EchoMode = textinput.EchoNormal
			} else {
				f.input.EchoMode = textinput.EchoPassword
			}
			return f, nil
		}
	}
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	return f, cmd
}

func (f *templateFlow) View(m *Model, width int) string {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.ModalTitle.Render(f.tpl.Label) + "\n\n")

	if f.preview != nil {
		b.WriteString(f.preview.Summary + "\n\n")
		for _, s := range f.preview.Stmts {
			b.WriteString(t.Table.Render(truncEnd(sanitize(s), 70)+";") + "\n")
		}
		hint := "enter continue · esc cancel"
		if f.preview.Danger == dbx.DangerHigh {
			hint = "enter continue (typed confirmation follows) · esc cancel"
		}
		b.WriteString("\n" + t.ModalDim.Render(hint))
		return b.String()
	}

	p := f.tpl.Params[f.step]
	b.WriteString(p.Label + ":\n")
	b.WriteString(f.input.View() + "\n")
	if f.errMsg != "" {
		b.WriteString("\n" + t.Danger.Render(f.errMsg) + "\n")
	}
	hint := "enter next · esc cancel"
	if p.Kind == dbx.ParamSecret {
		hint = "ctrl+r reveal · " + hint
	}
	b.WriteString("\n" + t.ModalDim.Render(hint))
	return b.String()
}

// --- maintenance picker (§3.5 maintenance row) -------------------------------

type MaintPicker struct {
	connID int64
	ops    []dbx.MaintOp
	cursor int
}

func (m *Model) openMaintenance() tea.Cmd {
	c := m.cursorConn()
	if c == nil {
		return m.note(levelWarn, "select a connection first")
	}
	ref := dbx.ObjectRef{}
	if n := m.currentNode(); n != nil {
		switch n.Kind {
		case dbx.KTable:
			ref = n.Ref
		case dbx.KDatabase:
			ref = dbx.ObjectRef{Database: n.Ref.Database}
		}
	}
	ops := dbx.MaintenanceFor(c.Engine, ref)
	if len(ops) == 0 {
		return m.note(levelInfo, "no maintenance operations here")
	}
	m.modal = &MaintPicker{connID: c.ID, ops: ops}
	return nil
}

func (mp *MaintPicker) Update(m *Model, msg tea.KeyMsg) (Modal, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		return nil, nil
	case "up", "k":
		if mp.cursor > 0 {
			mp.cursor--
		}
	case "down", "j":
		if mp.cursor < len(mp.ops)-1 {
			mp.cursor++
		}
	case "enter":
		m.modal = nil
		cmd := m.runPlan(mp.connID, mp.ops[mp.cursor].Plan)
		return m.modal, cmd
	}
	return mp, nil
}

func (mp *MaintPicker) View(m *Model, width int) string {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.ModalTitle.Render("Maintenance") + "\n\n")
	for i, op := range mp.ops {
		line := "  " + op.Label
		if i == mp.cursor {
			line = t.MenuItemOn.Render(pad(line, 40))
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + t.ModalDim.Render("enter run (confirms) · esc close"))
	return b.String()
}
