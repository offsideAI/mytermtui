// Command screenshot drives the real ui.Model headlessly — no PTY, no
// running app — to produce deterministic ANSI frames for the docs
// (rendered to PNG by scripts/ansi2png.py). Data comes from a temp
// SQLite fixture plus the in-memory fake Postgres driver.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	_ "modernc.org/sqlite"

	"github.com/offsideai/mydb/internal/config"
	"github.com/offsideai/mydb/internal/dbx/fake"
	"github.com/offsideai/mydb/internal/registry"
	"github.com/offsideai/mydb/internal/ui"
)

type scene struct {
	name string
	keys []string
}

func buildScenes() []scene {
	return []scene{
		// Browse the sqlite fixture down to a table.
		{"01-browser", []string{"down", "enter", "down", "enter", "down"}},
		// Data grid on users (orders sorts first inside Tables).
		{"02-data-grid", []string{"down", "enter", "down", "enter", "down", "down", "tab", "]"}},
		// SQL tab with a query typed and run.
		{"03-sql", append([]string{"down", "enter", "tab", "]", "]", "i"},
			append(runes("select email from users where id <= 3"), "esc", "ctrl+r")...)},
		// Fake multi-database server: scoped connection + Annex.
		// Rows: Local, app.db, prod-pg — two downs reach the pg connection.
		{"04-annex", []string{"down", "down", "enter"}},
		// Connection form with the URL field.
		{"05-conn-form", []string{"B"}},
		// Common-commands template picker.
		{"06-commands", []string{"down", "down", "enter", "C"}},
	}
}

func runes(s string) []string {
	out := make([]string, 0, len(s))
	for _, r := range s {
		out = append(out, string(r))
	}
	return out
}

func main() {
	outDir := flag.String("out", "frames", "directory for .ansi frames")
	flag.Parse()

	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fail(err)
	}
	for _, sc := range buildScenes() {
		frame, err := renderScene(sc)
		if err != nil {
			fail(fmt.Errorf("%s: %w", sc.name, err))
		}
		path := filepath.Join(*outDir, sc.name+".ansi")
		if err := os.WriteFile(path, []byte(frame), 0o644); err != nil {
			fail(err)
		}
		fmt.Println("wrote", path)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "screenshot:", err)
	os.Exit(1)
}

// renderScene builds a fresh model over a fresh fixture and applies the
// scene's key script.
func renderScene(sc scene) (string, error) {
	dir, err := os.MkdirTemp("", "mydb-shot-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "app.db")
	if err := buildFixture(dbPath); err != nil {
		return "", err
	}
	reg, err := registry.Open(filepath.Join(dir, "registry.db"))
	if err != nil {
		return "", err
	}
	defer reg.Close()
	if _, err := reg.Create(registry.Connection{
		Name: "app.db", Engine: "sqlite", Locality: "local", Path: dbPath,
	}); err != nil {
		return "", err
	}
	if _, err := reg.Create(registry.Connection{
		Name: "prod-pg", Engine: "postgres", Locality: "local",
		Host: "db.internal", Port: 5432, DBName: "appdb", Username: "admin",
	}); err != nil {
		return "", err
	}

	cfg := config.Default()
	cfg.Query.PageSize = 50
	m := ui.New(cfg, reg)
	m.InstallDriver("postgres", fake.New())

	apply(m, tea.WindowSizeMsg{Width: 120, Height: 34})
	drain(m, m.Init())
	for _, k := range sc.keys {
		apply(m, keyMsg(k))
	}
	return m.View(), nil
}

func buildFixture(path string) error {
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return err
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL, created_at TEXT)`,
		`CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL, total REAL)`,
		`CREATE INDEX idx_orders_user ON orders(user_id)`,
		`CREATE VIEW user_emails AS SELECT email FROM users`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	for i := 1; i <= 12; i++ {
		if _, err := db.Exec(`INSERT INTO users (email, created_at) VALUES (?, ?)`,
			fmt.Sprintf("user%02d@example.com", i),
			fmt.Sprintf("2026-07-%02d 09:00", i)); err != nil {
			return err
		}
	}
	return nil
}

// apply pumps one message through the Elm loop, then synchronously runs
// the returned commands (skipping parked ticks via a short timeout).
func apply(m *ui.Model, msg tea.Msg) {
	if msg == nil {
		return
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			drain(m, c)
		}
		return
	}
	_, cmd := m.Update(msg)
	drain(m, cmd)
}

func drain(m *ui.Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		apply(m, msg)
	case <-time.After(100 * time.Millisecond):
		// A sleeping tick — skip for a still frame. The timeout MUST stay
		// below the 150ms spinner tick or the pump recurses on ticks
		// forever while real commands starve behind them in the batch.
	}
}

func keyMsg(name string) tea.KeyMsg {
	switch name {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "ctrl+r":
		return tea.KeyMsg{Type: tea.KeyCtrlR}
	case "ctrl+h":
		return tea.KeyMsg{Type: tea.KeyCtrlH}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}
