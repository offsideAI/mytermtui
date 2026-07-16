package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/offsideai/mydb/internal/registry"
)

// connForm is the multi-field create/edit modal for saved connections.
// Visible fields follow the chosen engine; enter advances (submitting on
// the last field), ctrl+s submits from anywhere.
type connForm struct {
	editID   int64 // 0 = create
	engine   int   // index into engines
	locality int   // index into localities
	fields   map[string]*textinput.Model
	focus    int
	errMsg   string
}

var (
	engines    = []string{"sqlite", "postgres"}
	localities = []string{"local", "remote"}
)

// fieldOrder lists the visible field ids for the current engine. The URL
// field leads: pasting a whole connection string there fills the rest.
func (f *connForm) fieldOrder() []string {
	base := []string{"url", "name", "engine", "locality"}
	if engines[f.engine] == "sqlite" {
		return append(base, "path")
	}
	return append(base, "host", "port", "dbname", "username", "password")
}

var fieldLabels = map[string]string{
	"url":  "URL",
	"name": "Name", "engine": "Engine", "locality": "Section",
	"path": "File", "host": "Host", "port": "Port",
	"dbname": "Database", "username": "User", "password": "Password",
}

func newConnForm(m *Model, edit *registry.Connection) *connForm {
	f := &connForm{fields: map[string]*textinput.Model{}}
	mk := func(id, value string) {
		ti := textinput.New()
		ti.SetValue(value)
		ti.CharLimit = 512
		ti.Width = 40
		ti.Prompt = ""
		if id == "password" {
			ti.EchoMode = textinput.EchoPassword
		}
		f.fields[id] = &ti
	}
	mk("url", "")
	f.fields["url"].CharLimit = 2048
	mk("name", "")
	mk("path", "")
	mk("host", "")
	mk("port", "5432")
	mk("dbname", "")
	mk("username", "")
	mk("password", "")
	if edit != nil {
		f.editID = edit.ID
		f.fields["name"].SetValue(edit.Name)
		f.fields["path"].SetValue(edit.Path)
		f.fields["host"].SetValue(edit.Host)
		if edit.Port > 0 {
			f.fields["port"].SetValue(strconv.Itoa(edit.Port))
		}
		f.fields["dbname"].SetValue(edit.DBName)
		f.fields["username"].SetValue(edit.Username)
		f.fields["password"].SetValue(edit.Secret)
		f.engine = indexOf(engines, edit.Engine)
		f.locality = indexOf(localities, edit.Locality)
	}
	f.syncFocus()
	return f
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return 0
}

// syncFocus gives the focused text field the cursor and blurs the rest.
func (f *connForm) syncFocus() {
	order := f.fieldOrder()
	if f.focus >= len(order) {
		f.focus = len(order) - 1
	}
	for id, ti := range f.fields {
		if id == order[f.focus] {
			ti.Focus()
			ti.CursorEnd()
		} else {
			ti.Blur()
		}
	}
}

func (f *connForm) isChoice(id string) bool { return id == "engine" || id == "locality" }

func (f *connForm) cycle(id string, dir int) {
	switch id {
	case "engine":
		f.engine = (f.engine + dir + len(engines)) % len(engines)
	case "locality":
		f.locality = (f.locality + dir + len(localities)) % len(localities)
	}
}

func (f *connForm) Update(m *Model, msg tea.KeyMsg) (Modal, tea.Cmd) {
	order := f.fieldOrder()
	id := order[f.focus]
	key := msg.String()

	switch key {
	case "esc":
		return nil, nil
	case "ctrl+s":
		return f.submit(m)
	case "tab", "down":
		f.focus = (f.focus + 1) % len(order)
		f.syncFocus()
		return f, nil
	case "shift+tab", "up":
		f.focus = (f.focus + len(order) - 1) % len(order)
		f.syncFocus()
		return f, nil
	case "enter":
		if id == "url" {
			if strings.TrimSpace(f.fields["url"].Value()) != "" && !f.applyURL() {
				return f, nil // parse error shown; stay on the field
			}
			f.focus++
			f.syncFocus()
			return f, nil
		}
		if f.focus == len(order)-1 {
			return f.submit(m)
		}
		f.focus++
		f.syncFocus()
		return f, nil
	}

	if f.isChoice(id) {
		switch key {
		case "left", "h":
			f.cycle(id, -1)
		case "right", "l", " ", "space":
			f.cycle(id, +1)
		}
		f.syncFocus() // engine switch changes the field list
		return f, nil
	}

	if id == "path" && key == "ctrl+t" {
		ti := f.fields["path"]
		ti.SetValue(completePath(ti.Value(), m.home))
		ti.CursorEnd()
		return f, nil
	}
	if id == "password" && key == "ctrl+r" {
		ti := f.fields["password"]
		if ti.EchoMode == textinput.EchoPassword {
			ti.EchoMode = textinput.EchoNormal
		} else {
			ti.EchoMode = textinput.EchoPassword
		}
		return f, nil
	}
	ti := f.fields[id]
	var cmd tea.Cmd
	*ti, cmd = ti.Update(msg)
	return f, cmd
}

// applyURL parses the URL field into the individual fields (which stay
// editable). Returns false and sets errMsg on a malformed string.
func (f *connForm) applyURL() bool {
	p, err := parseConnString(f.fields["url"].Value())
	if err != nil {
		f.errMsg = err.Error()
		return false
	}
	f.errMsg = ""
	f.engine = indexOf(engines, p.Engine)
	set := func(id, v string) {
		if v != "" {
			f.fields[id].SetValue(v)
		}
	}
	set("path", p.Path)
	set("host", p.Host)
	if p.Port > 0 {
		f.fields["port"].SetValue(strconv.Itoa(p.Port))
	}
	set("dbname", p.DBName)
	set("username", p.Username)
	set("password", p.Password)
	return true
}

// submit validates and writes to the registry.
func (f *connForm) submit(m *Model) (Modal, tea.Cmd) {
	// A pasted-but-unparsed URL still counts: apply it before validating.
	if strings.TrimSpace(f.fields["url"].Value()) != "" && !f.applyURL() {
		return f, nil
	}
	c := registry.Connection{
		ID:       f.editID,
		Name:     strings.TrimSpace(f.fields["name"].Value()),
		Engine:   engines[f.engine],
		Locality: localities[f.locality],
		Path:     strings.TrimSpace(f.fields["path"].Value()),
		Host:     strings.TrimSpace(f.fields["host"].Value()),
		DBName:   strings.TrimSpace(f.fields["dbname"].Value()),
		Username: strings.TrimSpace(f.fields["username"].Value()),
		Secret:   f.fields["password"].Value(),
	}
	if p := strings.TrimSpace(f.fields["port"].Value()); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 65535 {
			f.errMsg = "port must be a number"
			return f, nil
		}
		c.Port = n
	}
	switch {
	case c.Name == "":
		f.errMsg = "name is required"
		return f, nil
	case c.Engine == "sqlite" && c.Path == "":
		f.errMsg = "file path is required"
		return f, nil
	case c.Engine == "postgres" && c.Host == "":
		f.errMsg = "host is required"
		return f, nil
	case c.Engine == "postgres" && c.DBName == "":
		f.errMsg = "database is required"
		return f, nil
	}
	if c.Engine == "postgres" && c.Port == 0 {
		c.Port = 5432
	}
	if f.editID == 0 {
		return nil, regCmd("saved "+c.Name, func() error {
			_, err := m.reg.Create(c)
			return err
		})
	}
	return nil, regCmd("updated "+c.Name, func() error { return m.reg.Update(c) })
}

func (f *connForm) View(m *Model, width int) string {
	t := m.theme
	title := "New connection"
	if f.editID != 0 {
		title = "Edit connection"
	}
	var b strings.Builder
	b.WriteString(t.ModalTitle.Render(title) + "\n\n")
	order := f.fieldOrder()
	for i, id := range order {
		label := pad(fieldLabels[id], 10)
		if i == f.focus {
			label = t.MenuItemOn.Render(label)
		} else {
			label = t.ModalDim.Render(label)
		}
		var control string
		switch id {
		case "engine":
			control = choiceView(t, engines, f.engine)
		case "locality":
			control = choiceView(t, localities, f.locality)
		default:
			control = f.fields[id].View()
		}
		b.WriteString(label + " " + control + "\n")
	}
	if f.errMsg != "" {
		b.WriteString("\n" + t.Danger.Render(f.errMsg) + "\n")
	}
	hint := "enter next/save · ctrl+s save · tab/↑↓ fields · esc cancel"
	if engines[f.engine] == "sqlite" {
		hint = "ctrl+t complete path · " + hint
	} else {
		hint = "ctrl+r reveal password · " + hint
	}
	if f.fieldOrder()[f.focus] == "url" {
		hint = "paste a connection string + enter fills the fields · " + hint
	}
	b.WriteString("\n" + t.ModalDim.Render(hint))
	return b.String()
}

func choiceView(t Theme, opts []string, sel int) string {
	parts := make([]string, len(opts))
	for i, o := range opts {
		if i == sel {
			parts[i] = t.ModalTitle.Render("● " + o)
		} else {
			parts[i] = t.ModalDim.Render("○ " + o)
		}
	}
	return strings.Join(parts, "   ")
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
