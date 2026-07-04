package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/offsideai/mytermtui/internal/config"
	"github.com/offsideai/mytermtui/internal/fsx"
	"github.com/offsideai/mytermtui/internal/icloud"
)

// drive prepares a model over a real tmpdir, loaded and sized.
func drive(t *testing.T, files map[string]string) *Model {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	queue := icloud.NewQueue(icloud.NewBridge(), 1, "")
	m := New(cfg, dir, icloud.NewBridge(), queue)

	step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	msg := loadDirCmd(dir, "")()
	step(t, m, msg)
	return m
}

func step(t *testing.T, m *Model, msg tea.Msg) tea.Cmd {
	t.Helper()
	model, cmd := m.Update(msg)
	if model.(*Model) != m {
		t.Fatal("model identity changed")
	}
	return cmd
}

func key(t *testing.T, m *Model, k string) tea.Cmd {
	t.Helper()
	var msg tea.KeyMsg
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "ctrl+w":
		msg = tea.KeyMsg{Type: tea.KeyCtrlW}
	case " ":
		msg = tea.KeyMsg{Type: tea.KeySpace}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	return step(t, m, msg)
}

func TestBrowseAndRender(t *testing.T) {
	m := drive(t, map[string]string{
		"alpha.txt": "hello",
		"beta.mov":  "movie",
		"sub/":      "",
	})
	view := m.View()
	if !strings.Contains(view, "alpha.txt") || !strings.Contains(view, "beta.mov") {
		t.Fatalf("view missing files:\n%s", view)
	}
	if !strings.Contains(view, "mytermtui") {
		t.Error("menu bar missing app title")
	}

	// dirs-first: cursor 0 is sub/, move down to alpha
	if m.cursorPath() == "" || filepath.Base(m.cursorPath()) != "sub" {
		t.Fatalf("cursor = %q, want sub first (dirs-first)", m.cursorPath())
	}
	key(t, m, "j")
	if filepath.Base(m.cursorPath()) != "alpha.txt" {
		t.Fatalf("cursor after j = %q", m.cursorPath())
	}
}

func TestSelectionAndStatus(t *testing.T) {
	m := drive(t, map[string]string{"a.txt": "1", "b.txt": "22", "c.txt": "333"})
	key(t, m, " ") // select a.txt, cursor moves to b
	key(t, m, " ") // select b.txt
	if len(m.selected) != 2 {
		t.Fatalf("selected = %d, want 2", len(m.selected))
	}
	if !strings.Contains(m.View(), "2 selected") {
		t.Error("status bar missing selection count")
	}
	key(t, m, "esc")
	if len(m.selected) != 0 {
		t.Error("esc did not clear selection")
	}
}

func TestFilterNarrowsView(t *testing.T) {
	m := drive(t, map[string]string{"walkthrough.mov": "x", "notes.txt": "y"})
	key(t, m, "f")
	if !m.filtering {
		t.Fatal("f did not enter filter mode")
	}
	for _, r := range "mov" {
		key(t, m, string(r))
	}
	if len(m.view) != 1 || filepath.Base(m.cursorPath()) != "walkthrough.mov" {
		t.Fatalf("filtered view = %d items, cursor %q", len(m.view), m.cursorPath())
	}
	key(t, m, "esc")
	if len(m.view) != 2 {
		t.Error("esc did not clear filter")
	}
}

func TestHiddenToggle(t *testing.T) {
	m := drive(t, map[string]string{"visible.txt": "x", ".secret": "y"})
	if len(m.view) != 1 {
		t.Fatalf("hidden files shown by default: %d", len(m.view))
	}
	key(t, m, "z")
	if len(m.view) != 2 {
		t.Fatalf("z did not reveal hidden files: %d", len(m.view))
	}
}

func TestEnterOnFileRevealsNotOpens(t *testing.T) {
	m := drive(t, map[string]string{"doc.txt": "x"})
	if m.cfg.General.EnterOpensFile != "reveal" {
		t.Fatalf("default enter_opens_file = %q, want reveal", m.cfg.General.EnterOpensFile)
	}
	cwd := m.cwd
	cmd := key(t, m, "enter") // cursor is on doc.txt (only entry)
	if cmd == nil {
		t.Fatal("enter on a file returned no command")
	}
	// Must not navigate, must not open a modal — it shells out to
	// `open -R` (not executed here).
	if m.cwd != cwd || m.modal != nil || m.loading {
		t.Fatalf("enter on file changed browser state: cwd=%q modal=%T loading=%v", m.cwd, m.modal, m.loading)
	}

	// With enter_opens_file = "app", plain files go to the app opener.
	m2 := drive(t, map[string]string{"doc.txt": "x"})
	m2.cfg.General.EnterOpensFile = "app"
	if cmd := key(t, m2, "enter"); cmd == nil {
		t.Fatal("enter (app mode) returned no command")
	}
}

func TestFolderGlyphAggregatesQueueState(t *testing.T) {
	m := drive(t, map[string]string{"sub/inner.mov": "x", "plain.txt": "y"})
	// Fake iCloud membership: glyphFor gates on InICloud, so point the
	// entries at paths and mark them as in-cloud.
	var sub *fsx.Entry
	for i := range m.entries {
		m.entries[i].InICloud = true
		if m.entries[i].Name == "sub" {
			sub = &m.entries[i]
		}
	}
	if sub == nil {
		t.Fatal("sub entry missing")
	}

	// A queued file inside sub → folder shows ◌.
	m.setQsnap(icloud.Snapshot{Items: []icloud.Item{
		{Path: filepath.Join(sub.Path, "inner.mov"), State: icloud.StateQueued},
	}})
	// qdirs aggregation only tracks ancestors under the iCloud root, so
	// substitute the map directly for this synthetic tmpdir case…
	if len(m.qdirs) != 0 {
		t.Fatal("tmpdir paths must not aggregate under the iCloud root")
	}
	m.qdirs[sub.Path] = icloud.StateQueued
	if g, _ := m.glyphFor(*sub); g != "◌" {
		t.Fatalf("queued folder glyph = %q, want ◌", g)
	}
	m.qdirs[sub.Path] = icloud.StateDownloading
	if g, _ := m.glyphFor(*sub); g != "⇣" {
		t.Fatalf("downloading folder glyph = %q, want ⇣", g)
	}
	delete(m.qdirs, sub.Path)
	if g, _ := m.glyphFor(*sub); g != "·" {
		t.Fatalf("idle folder glyph = %q, want ·", g)
	}
}

func TestQdirsAggregationUnderICloudRoot(t *testing.T) {
	m := drive(t, map[string]string{"a.txt": "x"})
	base := filepath.Join(icloud.MobileDocs(), "com~apple~CloudDocs", "proj", "deep")
	m.setQsnap(icloud.Snapshot{Items: []icloud.Item{
		{Path: filepath.Join(base, "one.mov"), State: icloud.StateQueued},
		{Path: filepath.Join(base, "two.mov"), State: icloud.StateDownloading},
	}})
	// The transferring descendant outranks the queued one on every
	// ancestor up to the iCloud root.
	for _, dir := range []string{base, filepath.Dir(base)} {
		if st, ok := m.qdirs[dir]; !ok || st != icloud.StateDownloading {
			t.Fatalf("qdirs[%q] = %v ok=%v, want downloading", dir, st, ok)
		}
	}
	if _, ok := m.qdirs["/"]; ok {
		t.Fatal("aggregation escaped the iCloud root")
	}
}

func TestDualPanelOpenSwapClose(t *testing.T) {
	m := drive(t, map[string]string{"sub/inner.txt": "x", "aaa.txt": "y"})
	root := m.cwd

	// → on the folder (cursor starts on "sub", dirs-first) opens it in
	// the right panel and focuses it.
	cmd := key(t, m, "right")
	if !m.split() || !m.focusRight {
		t.Fatalf("split=%v focusRight=%v after →", m.split(), m.focusRight)
	}
	step(t, m, cmd())
	if filepath.Base(m.cwd) != "sub" || m.other.cwd != root {
		t.Fatalf("cwd=%q other=%q", m.cwd, m.other.cwd)
	}

	// Select inside the right panel.
	key(t, m, " ")
	if len(m.selected) != 1 {
		t.Fatalf("right panel selection = %d", len(m.selected))
	}

	// Tab returns to the left panel; the right panel's selection and
	// cursor are remembered in the parked state.
	cmd = key(t, m, "tab")
	if m.focusRight || m.cwd != root {
		t.Fatalf("after tab: focusRight=%v cwd=%q", m.focusRight, m.cwd)
	}
	if len(m.other.selected) != 1 {
		t.Fatal("right panel selection was not remembered")
	}
	step(t, m, cmd()) // the reload of the newly focused panel

	// Tab again → right panel; ← steps focus back to the left panel.
	step(t, m, key(t, m, "tab")())
	if !m.focusRight {
		t.Fatal("second tab did not focus right panel")
	}
	cmd = key(t, m, "left")
	if m.focusRight {
		t.Fatal("← in right panel should focus the left panel")
	}
	step(t, m, cmd())

	// ctrl+w collapses to a single panel (left survives).
	key(t, m, "ctrl+w")
	if m.split() || m.cwd != root {
		t.Fatalf("after close: split=%v cwd=%q", m.split(), m.cwd)
	}

	// The split view renders both directory names side by side.
	step(t, m, key(t, m, "right")())
	view := m.View()
	if !strings.Contains(view, "inner.txt") || !strings.Contains(view, "aaa.txt") {
		t.Fatalf("split view missing panel contents:\n%s", view)
	}
}

func TestPaneResizeAndConfigRatio(t *testing.T) {
	m := drive(t, map[string]string{"sub/inner.txt": "x"})
	if m.ratio != 0.30 {
		t.Fatalf("default ratio = %v", m.ratio)
	}
	step(t, m, key(t, m, "right")())
	key(t, m, "<")
	if m.ratio != 0.25 {
		t.Fatalf("ratio after < = %v", m.ratio)
	}
	for i := 0; i < 20; i++ {
		key(t, m, ">")
	}
	if m.ratio > 0.85 {
		t.Fatalf("ratio not clamped: %v", m.ratio)
	}
}

func TestMenuOpensAndNavigates(t *testing.T) {
	m := drive(t, map[string]string{"a.txt": "x"})
	key(t, m, "m")
	if !m.menu.open {
		t.Fatal("menu did not open")
	}
	view := m.View()
	if !strings.Contains(view, "Open With…") {
		t.Errorf("File menu dropdown not rendered:\n%s", view)
	}
	key(t, m, "esc")
	if m.menu.open {
		t.Error("esc did not close menu")
	}
}

func TestHelpModal(t *testing.T) {
	m := drive(t, map[string]string{"a.txt": "x"})
	key(t, m, "?")
	if m.modal == nil {
		t.Fatal("help modal did not open")
	}
	if !strings.Contains(m.View(), "keyboard reference") {
		t.Error("help content missing")
	}
	key(t, m, "esc")
	if m.modal != nil {
		t.Error("esc did not close help")
	}
}

// runOp executes a command that may be a bare opCmd or a tea.Batch of
// opCmd + ticks, delivers the opDoneMsg to the model, and returns it.
func runOp(t *testing.T, m *Model, cmd tea.Cmd) opDoneMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected an operation command")
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c == nil {
				continue
			}
			inner := c()
			if od, ok := inner.(opDoneMsg); ok {
				step(t, m, od)
				return od
			}
		}
		t.Fatal("no opDoneMsg in batch")
	}
	od, ok := msg.(opDoneMsg)
	if !ok {
		t.Fatalf("op result = %#v", msg)
	}
	step(t, m, od)
	return od
}

func TestRenameFlow(t *testing.T) {
	m := drive(t, map[string]string{"old.txt": "content"})
	key(t, m, "r")
	im, ok := m.modal.(*InputModal)
	if !ok {
		t.Fatalf("modal = %T, want InputModal", m.modal)
	}
	im.Input.SetValue("new.txt")
	done := runOp(t, m, key(t, m, "enter"))
	if done.err != nil {
		t.Fatalf("rename failed: %v", done.err)
	}
	if _, err := os.Stat(filepath.Join(m.cwd, "new.txt")); err != nil {
		t.Error("renamed file missing")
	}
	// undo restores the old name
	done = runOp(t, m, key(t, m, "u"))
	if done.err != nil {
		t.Fatalf("undo failed: %v", done.err)
	}
	if _, err := os.Stat(filepath.Join(m.cwd, "old.txt")); err != nil {
		t.Error("undo did not restore old.txt")
	}
}

func TestNavigateBigToSmallDirNoPanic(t *testing.T) {
	files := map[string]string{"sub/only.txt": "x"}
	for i := 0; i < 40; i++ {
		files[fmt.Sprintf("file-%02d.txt", i)] = "x"
	}
	m := drive(t, files)
	key(t, m, "G") // cursor to the bottom (view index ~40)
	if m.cursor < 10 {
		t.Fatalf("cursor = %d, want near bottom", m.cursor)
	}
	// Loading a 1-entry dir with the deep stale cursor used to panic in
	// rebuildView via currentEntry indexing new entries with old view.
	cmd := m.navigateTo(filepath.Join(m.cwd, "sub"), "")
	step(t, m, cmd())
	if filepath.Base(m.cwd) != "sub" || len(m.view) != 1 {
		t.Fatalf("cwd=%q view=%d", m.cwd, len(m.view))
	}
}

func TestFailedNavigationKeepsHistory(t *testing.T) {
	m := drive(t, map[string]string{"sub/inner.txt": "x"})
	step(t, m, key(t, m, "enter")()) // into sub
	backLen := len(m.histBack)
	cwd := m.cwd

	cmd := m.navigateTo("/definitely-not-a-real-dir-xyz", "")
	step(t, m, cmd()) // load fails
	if m.cwd != cwd {
		t.Fatalf("cwd changed to %q on failed nav", m.cwd)
	}
	if len(m.histBack) != backLen || len(m.histFwd) != 0 {
		t.Fatalf("history corrupted: back=%d fwd=%d", len(m.histBack), len(m.histFwd))
	}
	// Back still returns to the original directory.
	step(t, m, key(t, m, "[")())
	if filepath.Base(m.cwd) == "sub" {
		t.Fatal("back did not leave sub after failed nav")
	}
}

func TestRangeAnchorClearedOnRebuild(t *testing.T) {
	m := drive(t, map[string]string{"a.txt": "1", "b.txt": "2", ".h": "3"})
	key(t, m, "v")
	if m.anchor < 0 {
		t.Fatal("v did not set anchor")
	}
	key(t, m, "z") // toggle hidden → rebuildView reindexes
	if m.anchor != -1 {
		t.Fatal("anchor survived a view rebuild — range would target wrong files")
	}
}

func TestHintBar(t *testing.T) {
	m := drive(t, map[string]string{"a.txt": "1"})
	view := m.View()
	for _, want := range []string{"download", "trash", "menu", "╭", "╰"} {
		if !strings.Contains(view, want) {
			t.Errorf("hint bar missing %q", want)
		}
	}
	baseline := m.listHeight()

	// Context-aware: menu hints replace browse hints while a menu is open.
	key(t, m, "m")
	if v := m.View(); !strings.Contains(v, "menus") || strings.Contains(v, "download ") {
		t.Error("menu-context hints not shown")
	}
	key(t, m, "esc")

	// H toggles the bar off and returns its rows to the list.
	key(t, m, "H")
	if strings.Contains(m.View(), "╭") {
		t.Error("H did not hide the hint bar")
	}
	if m.listHeight() != baseline+4 {
		t.Errorf("listHeight = %d, want %d after hiding hints", m.listHeight(), baseline+4)
	}
}

func TestMutatingActionBlockedWhileOpRunning(t *testing.T) {
	m := drive(t, map[string]string{"a.txt": "1"})
	m.op = &opState{desc: "busy"}
	key(t, m, "r")
	if m.modal != nil {
		t.Fatal("rename modal opened while an operation was running")
	}
	if m.status.text == "" {
		t.Fatal("expected a busy warning note")
	}
}

func TestSortModalChangesOrder(t *testing.T) {
	m := drive(t, map[string]string{"small.txt": "1", "large.txt": "4444"})
	key(t, m, "s")
	if _, ok := m.modal.(*SortModal); !ok {
		t.Fatalf("modal = %T", m.modal)
	}
	key(t, m, "j") // size
	key(t, m, "enter")
	key(t, m, "esc")
	if m.sortBy != fsx.SortSize {
		t.Fatalf("sortBy = %v", m.sortBy)
	}
}

func TestNavigateIntoDirAndBack(t *testing.T) {
	m := drive(t, map[string]string{"sub/inner.txt": "x"})
	// cursor on sub (dirs first) → enter
	cmd := key(t, m, "enter")
	if cmd == nil {
		t.Fatal("enter on dir returned no cmd")
	}
	step(t, m, cmd())
	if filepath.Base(m.cwd) != "sub" {
		t.Fatalf("cwd = %q, want sub", m.cwd)
	}
	cmd = key(t, m, "[") // history back
	step(t, m, cmd())
	if filepath.Base(m.cwd) == "sub" {
		t.Fatal("back did not leave sub")
	}
}
