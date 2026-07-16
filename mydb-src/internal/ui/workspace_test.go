package ui

import (
	"strings"
	"testing"
)

// browseToUsers connects and puts the cursor on the users table.
func browseToUsers(t *testing.T, m *Model) {
	t.Helper()
	press(t, m, "down", "enter") // connect
	press(t, m, "down", "enter") // expand Tables
	press(t, m, "down")          // onto users
	if n := m.currentNode(); n == nil || n.Name != "users" {
		t.Fatalf("cursor should be on users, is on %v", m.currentNode())
	}
}

func TestWorkspaceFocusAndTabs(t *testing.T) {
	m, _ := fixture(t)
	press(t, m, "tab")
	if !m.focusRight || m.tab != tabInfo {
		t.Fatalf("tab should focus the workspace on Info (focusRight=%v tab=%d)", m.focusRight, m.tab)
	}
	press(t, m, "]")
	if m.tab != tabData {
		t.Fatalf("] should switch to Data, tab=%d", m.tab)
	}
	press(t, m, "]")
	if m.tab != tabInfo {
		t.Fatalf("] should wrap back to Info, tab=%d", m.tab)
	}
	press(t, m, "tab")
	if m.focusRight {
		t.Fatal("tab should return focus to the tree")
	}
}

func TestDataGridPaging(t *testing.T) {
	m, _ := fixture(t)
	browseToUsers(t, m)

	press(t, m, "tab", "]") // focus workspace, switch to Data → loads page 0
	g := &m.grid
	if g.res == nil {
		t.Fatalf("grid did not load (err=%q loading=%v)", g.err, g.loading)
	}
	if len(g.res.Rows) != 10 || !g.res.Truncated || g.page != 0 {
		t.Fatalf("page 0: rows=%d truncated=%v page=%d", len(g.res.Rows), g.res.Truncated, g.page)
	}
	if g.res.Rows[0][0].S != "1" {
		t.Fatalf("page 0 should start at id 1, got %v", g.res.Rows[0][0])
	}

	press(t, m, "J") // next page
	if g.page != 1 || g.res.Rows[0][0].S != "11" {
		t.Fatalf("page 1: page=%d first=%v", g.page, g.res.Rows[0][0])
	}
	press(t, m, "J", "J") // page 2 (last, 5 rows), then no-op (not truncated)
	if g.page != 2 || len(g.res.Rows) != 5 || g.res.Truncated {
		t.Fatalf("page 2: page=%d rows=%d truncated=%v", g.page, len(g.res.Rows), g.res.Truncated)
	}
	press(t, m, "K") // back
	if g.page != 1 {
		t.Fatalf("K should page back, page=%d", g.page)
	}

	// Cell navigation and the full-value popup.
	press(t, m, "l", "j")
	if g.col != 1 || g.row != 1 {
		t.Fatalf("cell cursor: row=%d col=%d", g.row, g.col)
	}
	press(t, m, "enter")
	im, ok := m.modal.(*InfoModal)
	if !ok {
		t.Fatalf("enter should open the cell popup, modal=%#v", m.modal)
	}
	if !strings.Contains(strings.Join(im.Lines, "\n"), "user12@x.com") {
		t.Fatalf("popup should show the cell value, got %v", im.Lines)
	}
	press(t, m, "esc")

	// Frame renders the grid without panicking.
	frame := m.View()
	for _, want := range []string{"email", "user11@x.com", "rows 11–20 of ~25"} {
		if !strings.Contains(frame, want) {
			t.Errorf("frame missing %q", want)
		}
	}
}

func TestGridRebindsOnNewTable(t *testing.T) {
	m, _ := fixture(t)
	browseToUsers(t, m)
	press(t, m, "tab", "]") // grid on users
	first := m.grid.path

	press(t, m, "tab")           // back to tree
	press(t, m, "left", "left")  // collapse users? no — jump Tables, collapse
	press(t, m, "down", "enter") // Views group expand
	press(t, m, "down")          // user_emails view
	if n := m.currentNode(); n == nil || n.Name != "user_emails" {
		t.Fatalf("cursor should be on user_emails, is on %v", m.currentNode())
	}
	press(t, m, "tab") // workspace still on Data tab → rebind
	if m.grid.path == first {
		t.Fatal("grid should rebind to the newly selected view")
	}
	if m.grid.res == nil || len(m.grid.res.Rows) != 10 {
		t.Fatalf("view grid: %+v err=%q", m.grid.res, m.grid.err)
	}
}

func TestDisconnectClearsGrid(t *testing.T) {
	m, _ := fixture(t)
	browseToUsers(t, m)
	press(t, m, "tab", "]")
	if m.grid.res == nil {
		t.Fatal("precondition: grid loaded")
	}
	press(t, m, "tab") // back to tree (cursor still inside the connection)
	press(t, m, "ctrl+c")
	if m.grid.res != nil || m.grid.path != "" {
		t.Fatalf("disconnect should clear the grid: %+v", m.grid)
	}
}
