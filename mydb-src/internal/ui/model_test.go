package ui

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/offsideai/mydb/internal/config"
	"github.com/offsideai/mydb/internal/registry"
)

// The tests drive the real Model headlessly, the same way cmd/screenshot
// does in the sibling apps: run each returned command synchronously with
// a timeout (so sleeping ticks are skipped) and feed its message back.

func drain(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		apply(t, m, msg)
	case <-time.After(100 * time.Millisecond):
		// a parked tick (spinner, status expiry) — skip it
	}
}

func apply(t *testing.T, m *Model, msg tea.Msg) {
	t.Helper()
	if msg == nil {
		return
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			drain(t, m, c)
		}
		return
	}
	_, cmd := m.Update(msg)
	drain(t, m, cmd)
}

func press(t *testing.T, m *Model, keys ...string) {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "left":
			msg = tea.KeyMsg{Type: tea.KeyLeft}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		apply(t, m, msg)
	}
}

// fixture creates a sqlite database and a registry pointing at it.
func fixture(t *testing.T) (*Model, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL)`,
		`CREATE INDEX idx_users_email ON users(email)`,
		`CREATE VIEW user_emails AS SELECT email FROM users`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 25; i++ {
		if _, err := db.Exec(`INSERT INTO users (email) VALUES (?)`,
			fmt.Sprintf("user%02d@x.com", i)); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	reg, err := registry.Open(filepath.Join(dir, "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	if _, err := reg.Create(registry.Connection{
		Name: "app", Engine: "sqlite", Locality: "local", Path: dbPath,
	}); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Query.PageSize = 10 // small pages so grid paging is testable
	m := New(cfg, reg)
	apply(t, m, tea.WindowSizeMsg{Width: 120, Height: 34})
	drain(t, m, m.Init())
	return m, dbPath
}

// visible returns the flattened row names in display order.
func visible(m *Model) []string {
	out := make([]string, 0, len(m.view))
	for _, ni := range m.view {
		out = append(out, m.displayName(m.nodes[ni]))
	}
	return out
}

func TestLaunchShowsSectionsAndConnections(t *testing.T) {
	m, _ := fixture(t)
	rows := visible(m)
	want := []string{"Local", "app", "Local (Annex)", "Remote", "Remote (Annex)", "Roles"}
	if len(rows) != len(want) {
		t.Fatalf("rows = %v, want %v", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Fatalf("rows = %v, want %v", rows, want)
		}
	}
}

func TestBrowseDownToColumns(t *testing.T) {
	m, _ := fixture(t)

	press(t, m, "down")  // onto the connection
	press(t, m, "enter") // connect + load schema

	if n := m.currentNode(); n == nil || m.state[n.ConnID] != connOpen {
		t.Fatal("connection did not open")
	}
	rows := visible(m)
	if !contains(rows, "Tables (1)") || !contains(rows, "Views (1)") {
		t.Fatalf("groups missing after connect: %v", rows)
	}

	press(t, m, "down", "enter") // expand Tables
	rows = visible(m)
	if !contains(rows, "users") {
		t.Fatalf("users table missing: %v", rows)
	}

	press(t, m, "down", "enter") // expand users → Columns/Indexes groups
	rows = visible(m)
	if !contains(rows, "Columns") || !contains(rows, "Indexes") {
		t.Fatalf("table subgroups missing: %v", rows)
	}

	press(t, m, "down", "enter") // expand Columns (async fetch)
	rows = visible(m)
	if !contains(rows, "id") || !contains(rows, "email") {
		t.Fatalf("columns missing: %v", rows)
	}

	// The rendered frame should show it all without panicking.
	frame := m.View()
	for _, s := range []string{"users", "email", "Tables (1)"} {
		if !strings.Contains(frame, s) {
			t.Errorf("rendered view missing %q", s)
		}
	}
}

func TestCollapseAndParentJump(t *testing.T) {
	m, _ := fixture(t)
	press(t, m, "down", "enter") // connect
	press(t, m, "down", "enter") // expand Tables
	press(t, m, "down")          // onto users

	press(t, m, "left") // not expanded → jump to parent (Tables)
	if n := m.currentNode(); n == nil || !strings.HasPrefix(m.displayName(*n), "Tables") {
		t.Fatalf("← should jump to Tables, cursor on %v", m.currentNode())
	}
	press(t, m, "left") // expanded → collapse
	if contains(visible(m), "users") {
		t.Fatal("Tables should be collapsed")
	}
}

func TestFilterNarrowsNamedRows(t *testing.T) {
	m, _ := fixture(t)
	press(t, m, "down", "enter") // connect
	press(t, m, "down", "enter") // expand Tables

	press(t, m, "f", "u", "s", "e", "r", "s", "enter")
	rows := visible(m)
	if !contains(rows, "users") {
		t.Fatalf("filter lost the match: %v", rows)
	}
	if !contains(rows, "app") { // ancestors stay as context for matches
		t.Fatalf("filter dropped the match's connection: %v", rows)
	}
	if contains(rows, "Views (1)") { // no matching descendants → hidden
		t.Fatalf("filter kept a group with no matches: %v", rows)
	}
	press(t, m, "esc")
	if !contains(visible(m), "app") {
		t.Fatal("esc should clear the filter")
	}
}

func TestDisconnectDropsSubtree(t *testing.T) {
	m, _ := fixture(t)
	press(t, m, "down", "enter") // connect
	if !contains(visible(m), "Tables (1)") {
		t.Fatal("precondition: schema loaded")
	}
	press(t, m, "d")
	if contains(visible(m), "Tables (1)") {
		t.Fatalf("disconnect left schema rows: %v", visible(m))
	}
	if n := m.currentNode(); n == nil || m.state[n.ConnID] != connClosed {
		t.Fatal("connection state should be closed")
	}
}

func TestConnFormCreatesConnection(t *testing.T) {
	m, dbPath := fixture(t)

	press(t, m, "B")
	if _, ok := m.modal.(*connForm); !ok {
		t.Fatal("B should open the connection form")
	}
	press(t, m, "enter")                      // skip the empty URL field
	press(t, m, "s", "e", "c", "o", "n", "d") // name
	press(t, m, "enter")                      // → engine (sqlite already)
	press(t, m, "enter")                      // → locality (local already)
	press(t, m, "enter")                      // → access (read-write already)
	press(t, m, "enter")                      // → path
	for _, r := range dbPath {
		press(t, m, string(r)) // path field
	}
	press(t, m, "enter") // last field → submit

	if m.modal != nil {
		t.Fatalf("form should close on save (modal=%#v)", m.modal)
	}
	if !contains(visible(m), "second") {
		t.Fatalf("new connection missing: %v", visible(m))
	}
}

func TestEditKeyOpensPrefilledConnForm(t *testing.T) {
	m, _ := fixture(t)
	press(t, m, "down") // onto the connection
	press(t, m, "e")
	f, ok := m.modal.(*connForm)
	if !ok {
		t.Fatal("e should open the connection form")
	}
	if f.editID == 0 {
		t.Fatal("form should be in edit mode, not create")
	}
	if got := f.fields["name"].Value(); got != "app" {
		t.Fatalf("name not prefilled: %q", got)
	}
	press(t, m, "ctrl+s") // save unchanged
	if m.modal != nil {
		t.Fatalf("form should close on save (modal=%#v)", m.modal)
	}
	conns, err := m.reg.Connections()
	if err != nil || len(conns) != 1 || conns[0].Name != "app" {
		t.Fatalf("connection not persisted intact: %v %v", conns, err)
	}
}

func TestDeleteConnRequiresTypedName(t *testing.T) {
	m, _ := fixture(t)
	press(t, m, "down") // onto the connection
	press(t, m, "X")
	if _, ok := m.modal.(*TypedConfirmModal); !ok {
		t.Fatal("X should open the typed confirmation")
	}
	press(t, m, "enter") // nothing typed → must stay open
	if m.modal == nil {
		t.Fatal("empty confirmation must not delete")
	}
	press(t, m, "a", "p", "p", "enter")
	if m.modal != nil {
		t.Fatal("typed name should confirm")
	}
	if contains(visible(m), "app") {
		t.Fatalf("connection not deleted: %v", visible(m))
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
