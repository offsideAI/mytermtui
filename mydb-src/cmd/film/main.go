// Command film drives the real mydb ui.Model through a scripted
// storyboard, writing a numbered true-color ANSI frame per beat (with
// "hold" repeats for timing). scripts/ansi2png.py renders the frames to
// PNGs, then ffmpeg assembles them into a demo GIF. Headless — no PTY,
// no running app — the same technique as cmd/screenshot. Data is a temp
// SQLite fixture plus the in-memory fake Postgres driver.
//
//	go -C mydb-src run ./cmd/film -out ../_demo/frames
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

const (
	cols = 118
	rows = 32
)

type beat struct {
	keys []string
	hold int
}

func k(hold int, keys ...string) beat { return beat{keys: keys, hold: hold} }

func typeRunes(hold int, s string) []beat {
	var out []beat
	for _, r := range s {
		out = append(out, k(1, string(r)))
	}
	if len(out) > 0 {
		out[len(out)-1].hold = hold
	}
	return out
}

// storyboard tours mydb end to end.
func storyboard() []beat {
	var b []beat
	add := func(bs ...beat) { b = append(b, bs...) }
	addAll := func(bs []beat) { b = append(b, bs...) }

	// --- Act 1: connect a SQLite database and browse its schema ---
	add(k(8))                 // opening: Local/Remote sections, saved connections
	add(k(3, "down"))         // → app.db
	add(k(6, "enter"))        // connect + expand (green ● indicator; Tables/Views)
	add(k(3, "down"))         // → Tables
	add(k(5, "enter"))        // expand Tables (orders, users)
	add(k(3, "down"))         // → orders
	add(k(3, "down"))         // → users
	add(k(5, "enter"))        // expand users (Columns, Indexes)
	add(k(3, "down"))         // → Columns
	add(k(6, "enter"))        // expand Columns — Info panel shows column details
	add(k(3, "up"))           // ← back onto the users table

	// --- Data grid: read-only, paged ---
	add(k(3, "tab"))          // focus the workspace (Info tab)
	add(k(6, "]"))            // → Data tab: paged grid loads
	add(k(6, "J"))            // next page

	// --- SQL runner: write and run a query ---
	add(k(3, "]"))            // → SQL tab (vim editor)
	add(k(2, "i"))            // insert mode
	addAll(typeRunes(3, "select email from users where id <= 5"))
	add(k(3, "esc"))          // normal mode
	add(k(8, "ctrl+r"))       // run → results grid

	// --- Act 2: switch to PostgreSQL (one active connection) ---
	add(k(3, "ctrl+w"))       // jump back to the tree
	add(k(2, "g"))            // top
	add(k(3, "down"))         // → app.db
	add(k(4, "enter"))        // collapse it (compact tree)
	add(k(3, "down"))         // → prod-pg
	add(k(7, "c"))            // connect pg (disconnects app.db; indicator flips; schemas + Roles + Annex appear)

	// --- Common commands: templated admin ---
	add(k(7, "C"))            // template picker: Create user / database / grant …
	add(k(3, "esc"))

	// --- Maintenance ---
	add(k(6, "M"))            // VACUUM / ANALYZE / REINDEX …
	add(k(3, "esc"))

	// --- Backup into a background job ---
	add(k(7, "b"))            // backup path dialog
	add(k(3, "esc"))

	add(k(8))                 // final resting frame
	return b
}

func main() {
	out := flag.String("out", "../_demo/frames", "output directory for .ansi frames")
	theme := flag.String("theme", "default", "theme name")
	flag.Parse()

	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fail(err)
	}

	dir, err := os.MkdirTemp("", "mydb-film-*")
	if err != nil {
		fail(err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "app.db")
	if err := buildFixture(dbPath); err != nil {
		fail(err)
	}
	reg, err := registry.Open(filepath.Join(dir, "registry.db"))
	if err != nil {
		fail(err)
	}
	defer reg.Close()
	mustCreate(reg, registry.Connection{Name: "app.db", Engine: "sqlite", Locality: "local", Path: dbPath})
	mustCreate(reg, registry.Connection{Name: "prod-pg", Engine: "postgres", Locality: "local",
		Host: "db.internal", Port: 5432, DBName: "appdb", Username: "admin"})

	cfg := config.Default()
	cfg.Theme.Name = *theme
	cfg.Query.PageSize = 8 // two pages over the 12-row users table
	cfg.Backup.Dir = dir   // keep the backup dialog's default path tidy
	m := ui.New(cfg, reg)
	m.InstallDriver("postgres", fake.New())

	apply(m, tea.WindowSizeMsg{Width: cols, Height: rows})
	drain(m, m.Init())

	frame := 0
	emit := func(n int) {
		view := m.View()
		for i := 0; i < n; i++ {
			path := filepath.Join(*out, fmt.Sprintf("f%04d.ansi", frame))
			if err := os.WriteFile(path, []byte(view), 0o644); err != nil {
				fail(err)
			}
			frame++
		}
	}

	for _, bt := range storyboard() {
		for _, key := range bt.keys {
			apply(m, keyMsg(key))
		}
		hold := bt.hold
		if hold < 1 {
			hold = 1
		}
		emit(hold)
	}
	fmt.Printf("wrote %d frames to %s\n", frame, *out)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "film:", err)
	os.Exit(1)
}

func mustCreate(reg *registry.Registry, c registry.Connection) {
	if _, err := reg.Create(c); err != nil {
		fail(err)
	}
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

// apply pumps one message through the Elm loop, then chases commands.
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
		// a sleeping tick — skip for a still frame
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
	case "ctrl+w":
		return tea.KeyMsg{Type: tea.KeyCtrlW}
	case "ctrl+h":
		return tea.KeyMsg{Type: tea.KeyCtrlH}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}
