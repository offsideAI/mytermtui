package ui

import (
	"strings"
	"testing"
)

// toSQLTab connects the fixture's sqlite connection and focuses the SQL
// tab (editor focused).
func toSQLTab(t *testing.T, m *Model) *sqlSession {
	t.Helper()
	press(t, m, "down", "enter") // connect
	press(t, m, "tab", "]", "]") // workspace → Data → SQL
	if m.tab != tabSQL || !m.focusRight {
		t.Fatalf("should be focused on the SQL tab (tab=%d focusRight=%v)", m.tab, m.focusRight)
	}
	s := m.currentSQLSession(false)
	if s == nil || !s.editorFocused {
		t.Fatalf("SQL session should exist with the editor focused: %+v", s)
	}
	return s
}

func typeSQL(t *testing.T, m *Model, sql string) {
	t.Helper()
	press(t, m, "i")
	for _, r := range sql {
		press(t, m, string(r))
	}
	press(t, m, "esc")
}

func TestSQLRunBuffer(t *testing.T) {
	m, _ := fixture(t)
	s := toSQLTab(t, m)

	typeSQL(t, m, "select email from users where id <= 3")
	press(t, m, "ctrl+r")

	if s.errText != "" {
		t.Fatalf("query failed: %s", s.errText)
	}
	if s.res == nil || len(s.res.Rows) != 3 || s.res.Stmts != 1 {
		t.Fatalf("result wrong: %+v", s.res)
	}
	frame := m.View()
	for _, want := range []string{"user01@x.com", "3 row(s)", "1 stmt(s)"} {
		if !strings.Contains(frame, want) {
			t.Errorf("frame missing %q", want)
		}
	}

	// History was recorded.
	hist, err := m.reg.History(0, 10)
	if err != nil || len(hist) != 1 || !hist[0].OK || hist[0].Rows != 3 {
		t.Fatalf("history wrong: %+v err=%v", hist, err)
	}
}

func TestSQLColonWRuns(t *testing.T) {
	m, _ := fixture(t)
	s := toSQLTab(t, m)
	typeSQL(t, m, "select count(*) from users")
	press(t, m, ":", "w", "enter")
	if s.res == nil || len(s.res.Rows) != 1 || s.res.Rows[0][0].S != "25" {
		t.Fatalf(":w did not run: %+v err=%s", s.res, s.errText)
	}
}

func TestSQLErrorShown(t *testing.T) {
	m, _ := fixture(t)
	s := toSQLTab(t, m)
	typeSQL(t, m, "select * from nope")
	press(t, m, "ctrl+r")
	if s.errText == "" || s.res != nil {
		t.Fatalf("error not surfaced: res=%v err=%q", s.res, s.errText)
	}
	if !strings.Contains(m.View(), "nope") {
		t.Error("frame should show the error")
	}
	// The failed run lands in history too.
	hist, _ := m.reg.History(0, 10)
	if len(hist) != 1 || hist[0].OK || hist[0].Error == "" {
		t.Fatalf("failed run missing from history: %+v", hist)
	}
}

func TestSQLScriptTotals(t *testing.T) {
	m, _ := fixture(t)
	s := toSQLTab(t, m)
	typeSQL(t, m, "update users set email = 'x@y.z' where id = 1; select email from users where id = 1")
	press(t, m, "ctrl+r")
	if s.errText != "" || s.res == nil {
		t.Fatalf("script failed: %q", s.errText)
	}
	if s.res.Stmts != 2 || s.res.Affected != 1 || s.res.Rows[0][0].S != "x@y.z" {
		t.Fatalf("script totals wrong: %+v", s.res)
	}
}

func TestSQLExplain(t *testing.T) {
	m, _ := fixture(t)
	s := toSQLTab(t, m)
	typeSQL(t, m, "select * from users")
	press(t, m, "tab") // editor normal → results focus
	if s.editorFocused {
		t.Fatal("tab should focus the results pane")
	}
	press(t, m, "e")
	if !strings.HasPrefix(s.lastSQL, "EXPLAIN QUERY PLAN ") {
		t.Fatalf("explain prefix missing: %q", s.lastSQL)
	}
	if s.errText != "" || s.res == nil || len(s.res.Rows) == 0 {
		t.Fatalf("explain produced nothing: %+v %q", s.res, s.errText)
	}
	press(t, m, "E") // sqlite has no ANALYZE variant → warn, no modal
	if m.modal != nil {
		t.Fatal("sqlite EXPLAIN ANALYZE should not open a confirm")
	}
}

func TestSQLCancelAndRunConfirm(t *testing.T) {
	m, _ := fixture(t)
	s := toSQLTab(t, m)
	typeSQL(t, m, "select 1")
	s.running = true // simulate an in-flight query
	press(t, m, "ctrl+r")
	if _, ok := m.modal.(*ConfirmModal); !ok {
		t.Fatalf("running query + run should confirm, modal=%#v", m.modal)
	}
	press(t, m, "y") // cancel-and-run
	if s.running && s.res == nil {
		t.Fatal("confirmed run did not execute")
	}
}

func TestSQLHistoryModalLoads(t *testing.T) {
	m, _ := fixture(t)
	s := toSQLTab(t, m)
	typeSQL(t, m, "select 41")
	press(t, m, "ctrl+r")
	press(t, m, ":", "q", "enter") // leave the editor → tree focus
	if m.focusRight {
		t.Fatal(":q should park focus on the tree")
	}

	press(t, m, "ctrl+h")
	hm, ok := m.modal.(*HistoryModal)
	if !ok {
		t.Fatalf("ctrl+h should open history, modal=%#v", m.modal)
	}
	if !hm.loaded || len(hm.filtered) != 1 {
		t.Fatalf("history modal state: loaded=%v filtered=%d", hm.loaded, len(hm.filtered))
	}
	press(t, m, "4", "1") // fuzzy filter still matches "select 41"
	if len(hm.filtered) != 1 {
		t.Fatalf("fuzzy filter lost the entry: %d", len(hm.filtered))
	}
	press(t, m, "enter")
	if m.modal != nil || !m.focusRight || m.tab != tabSQL {
		t.Fatal("enter should load into the SQL tab")
	}
	if got := strings.TrimSpace(string(s.editor.Content())); got != "select 41" {
		// the session's editor was replaced on load
		got = strings.TrimSpace(string(m.currentSQLSession(false).editor.Content()))
		if got != "select 41" {
			t.Fatalf("editor content = %q", got)
		}
	}
}
