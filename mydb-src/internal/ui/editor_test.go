package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func ed(t *testing.T, text string) *Editor {
	t.Helper()
	return newEditor("/tmp/f.txt", []byte(text), false)
}

// feed sends a sequence of keys; runes go one per key, with a few named
// keys spelled as <esc>, <cr>, <bs>, <tab>, <c-r>.
func feed(t *testing.T, e *Editor, keys string) {
	t.Helper()
	i := 0
	for i < len(keys) {
		if keys[i] == '<' {
			j := strings.IndexByte(keys[i:], '>')
			name := keys[i+1 : i+j]
			i += j + 1
			var msg tea.KeyMsg
			switch name {
			case "esc":
				msg = tea.KeyMsg{Type: tea.KeyEsc}
			case "cr":
				msg = tea.KeyMsg{Type: tea.KeyEnter}
			case "bs":
				msg = tea.KeyMsg{Type: tea.KeyBackspace}
			case "tab":
				msg = tea.KeyMsg{Type: tea.KeyTab}
			case "c-r":
				msg = tea.KeyMsg{Type: tea.KeyCtrlR}
			default:
				t.Fatalf("unknown key <%s>", name)
			}
			e.Update(nil, msg)
			continue
		}
		e.Update(nil, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune(keys[i])}})
		i++
	}
}

func text(e *Editor) string {
	var b strings.Builder
	for i, l := range e.lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(string(l))
	}
	return b.String()
}

func TestEditorInsertAndText(t *testing.T) {
	e := ed(t, "hello")
	feed(t, e, "A world<esc>")
	if got := text(e); got != "hello world" {
		t.Fatalf("A: %q", got)
	}
	feed(t, e, "0ihi <esc>")
	if got := text(e); got != "hi hello world" {
		t.Fatalf("i: %q", got)
	}
	if !e.modified {
		t.Fatal("modified flag not set")
	}
}

func TestEditorOpenLineAndMotions(t *testing.T) {
	e := ed(t, "one\ntwo\nthree")
	feed(t, e, "Gothree-and-a-half<esc>")
	if got := text(e); got != "one\ntwo\nthree\nthree-and-a-half" {
		t.Fatalf("o at G: %q", got)
	}
	feed(t, e, "ggIx <esc>")
	if got := text(e); !strings.HasPrefix(got, "x one") {
		t.Fatalf("gg + I: %q", got)
	}
}

func TestEditorDeleteChangeWord(t *testing.T) {
	e := ed(t, "alpha beta gamma")
	feed(t, e, "dw")
	if got := text(e); got != "beta gamma" {
		t.Fatalf("dw: %q", got)
	}
	feed(t, e, "cwX<esc>")
	if got := text(e); got != "X gamma" {
		t.Fatalf("cw: %q", got)
	}
}

func TestEditorDeleteLineAndPaste(t *testing.T) {
	e := ed(t, "a\nb\nc")
	feed(t, e, "jdd") // delete line "b"
	if got := text(e); got != "a\nc" {
		t.Fatalf("dd: %q", got)
	}
	feed(t, e, "p") // paste "b" below current (now "c")
	if got := text(e); got != "a\nc\nb" {
		t.Fatalf("p: %q", got)
	}
}

func TestEditorUndoRedo(t *testing.T) {
	e := ed(t, "keep")
	feed(t, e, "ddu") // delete the only line, then undo
	if got := text(e); got != "keep" {
		t.Fatalf("undo: %q", got)
	}
	feed(t, e, "U") // redo the delete (ctrl+r runs SQL in mydb)
	if got := text(e); got != "" {
		t.Fatalf("redo: %q", got)
	}
}

func TestEditorCountedMotionAndDelete(t *testing.T) {
	e := ed(t, "0123456789")
	feed(t, e, "3lx") // move right 3, delete char '3'
	if got := text(e); got != "012456789" {
		t.Fatalf("3l x: %q", got)
	}
}

func TestEditorExQuit(t *testing.T) {
	e := ed(t, "hi")
	feed(t, e, "x") // modify (delete 'h')
	// The SQL buffer persists in its session, so :q parks focus even on
	// a modified buffer (mydb deviation from the file-editor original).
	feed(t, e, ":q<cr>")
	if !e.quit {
		t.Fatal(":q did not quit")
	}
	e.quit = false
	feed(t, e, ":nonsense<cr>")
	if e.quit || !strings.Contains(e.status, "not an editor command") {
		t.Fatalf("unknown ex command handling: quit=%v status=%q", e.quit, e.status)
	}
}

// :w routing into the SQL runner is covered by the sql_wire tests; here
// we only assert the ex-command plumbing stays intact with no model.
func TestEditorWriteCommandNilModelSafe(t *testing.T) {
	e := ed(t, "data")
	feed(t, e, ":w<cr>") // must not panic without a model/session
	if e.quit {
		t.Fatal(":w should not quit")
	}
}

func TestEditorSearch(t *testing.T) {
	e := ed(t, "one\ntwo\nthree\ntwo")
	feed(t, e, "/two<cr>")
	if e.row != 1 {
		t.Fatalf("/two landed on row %d, want 1", e.row)
	}
	feed(t, e, "n")
	if e.row != 3 {
		t.Fatalf("n landed on row %d, want 3", e.row)
	}
}

func TestEditorContentHasTrailingNewline(t *testing.T) {
	e := ed(t, "line")
	if got := string(e.Content()); got != "line\n" {
		t.Fatalf("Content = %q", got)
	}
}
