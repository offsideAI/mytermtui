package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/offsideai/mydb/internal/config"
	"github.com/offsideai/mydb/internal/registry"
)

func TestRolesTopLevelSection(t *testing.T) {
	m, _ := fixture(t)
	fakePGFixture(t, m, "pgc", "appdb")
	press(t, m, "enter") // connect

	rows := visible(m)
	// The connection's children are schemas only — no roles group, and no
	// role leaves anywhere inside the connection subtree.
	ci := indexOf2(t, m, "pgc")
	for vi := ci + 1; vi < len(m.view) && m.nodes[m.view[vi]].Depth > m.nodes[m.view[ci]].Depth; vi++ {
		if m.nodes[m.view[vi]].Name == "Roles" || m.nodes[m.view[vi]].Name == "admin" {
			t.Fatalf("roles must not nest under the connection: %v", rows)
		}
	}
	// The Roles section holds one server entry, labeled by the HOST.
	ri := indexOf2(t, m, "Roles")
	server := m.nodes[m.view[ri+1]]
	if server.Name != "srv" || server.Meta.Target != "srv:5432" {
		t.Fatalf("roles server entry wrong: %+v", server)
	}
	if contains(rows, "admin") {
		t.Fatal("role leaves must load lazily")
	}

	m.cursor = ri + 1
	press(t, m, "enter") // expand the server → role leaves
	if !contains(visible(m), "admin") {
		t.Fatalf("expanding the roles server should list roles: %v", visible(m))
	}
}

func TestRolesDedupeAcrossConnections(t *testing.T) {
	m, _ := fixture(t)
	fakePGFixture(t, m, "pgc", "appdb")
	press(t, m, "enter")
	fakePGFixture(t, m, "pgc2", "postgres") // same srv:5432
	press(t, m, "enter")

	count := 0
	ri := indexOf2(t, m, "Roles")
	for vi := ri + 1; vi < len(m.view); vi++ {
		if m.nodes[m.view[vi]].Meta.Target == "srv:5432" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("one server must appear once in Roles, got %d", count)
	}
}

func TestRolesClearOnDisconnect(t *testing.T) {
	m, _ := fixture(t)
	fakePGFixture(t, m, "pgc", "appdb")
	press(t, m, "enter")
	if len(m.childCache["sec:roles"]) != 1 {
		t.Fatal("precondition: roles server listed")
	}
	m.cursor = indexOf2(t, m, "pgc")
	press(t, m, "d")
	if len(m.childCache["sec:roles"]) != 0 {
		t.Fatalf("disconnect must clear the Roles section: %v", m.childCache["sec:roles"])
	}
}

func TestConnectionDetailUntruncated(t *testing.T) {
	dir := t.TempDir()
	reg, err := registry.Open(dir + "/registry.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	if _, err := reg.Create(registry.Connection{
		Name: "hopscotchgo", Engine: "postgres", Locality: "local",
		Host: "localhost", Port: 5432, DBName: "hopscotchgo_dev",
	}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	if cfg.General.SplitRatio != 0.50 {
		t.Fatalf("default split_ratio = %v, want 0.50", cfg.General.SplitRatio)
	}
	m := New(cfg, reg)
	apply(t, m, tea.WindowSizeMsg{Width: 160, Height: 34})
	drain(t, m, m.Init())

	full := "postgres · localhost:5432/hopscotchgo_dev"
	if !strings.Contains(m.View(), full) {
		t.Fatalf("connection target should render untruncated:\n%s", m.View())
	}
}
