// Command film drives the real myconsole ui.Model through a scripted
// storyboard, writing a numbered true-color ANSI frame per beat (with
// "hold" repeats to control timing). scripts/ansi2png.py renders the
// frames to PNGs, then ffmpeg assembles them into a demo GIF. Headless:
// no PTY, no running app — the same technique as cmd/screenshot.
//
//	go -C myconsole-src run ./cmd/film -dir "<start dir>" -out ../_demo/frames
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/offsideai/myconsole/internal/config"
	"github.com/offsideai/myconsole/internal/icloud"
	"github.com/offsideai/myconsole/internal/ui"
)

const (
	cols = 100
	rows = 30
)

// beat is one storyboard step: apply keys, then hold the resulting frame
// for `hold` frames (so important moments linger in the GIF).
type beat struct {
	keys []string
	hold int
}

func k(hold int, keys ...string) beat { return beat{keys: keys, hold: hold} }

// storyboard tours myconsole's functionality end to end. Cursor math
// assumes the dirs-first listing: assets(0) docs(1) src(2) go.mod(3)
// main.go(4) notes.txt(5) README.md(6).
func storyboard() []beat {
	typeRunes := func(hold int, s string) []beat {
		var out []beat
		for _, r := range s {
			out = append(out, k(1, string(r)))
		}
		if len(out) > 0 {
			out[len(out)-1].hold = hold
		}
		return out
	}
	var b []beat
	add := func(bs ...beat) { b = append(b, bs...) }
	addAll := func(bs []beat) { b = append(b, bs...) }

	// --- Browse: the detail panel follows the cursor ---
	add(k(7))                       // opening tree (cursor on assets, panel shows its contents)
	add(k(4, "j"))                  // → docs (panel shows guide.md / api.md)
	add(k(4, "j"))                  // → src  (panel shows server.go / client.go)

	// --- Expand a folder in place (nvim-tree style) ---
	add(k(6, "enter"))             // expand src — children indented beneath it
	add(k(4, "j"))                 // → client.go (nested); panel shows a safe text preview
	add(k(5, "j"))                 // → server.go
	add(k(4, "left"))              // ← jump to the parent row (src)
	add(k(6, "enter"))             // collapse src again

	// --- Filter live as you type ---
	add(k(4, "f"))                 // open the filter
	addAll(typeRunes(6, "go"))     // narrows to go.mod / main.go
	add(k(4, "esc"))               // clear it

	// --- Menu bar (every item shows its shortcut) ---
	add(k(5, "m"))                 // File menu
	add(k(4, "right"))             // → Edit
	add(k(4, "right"))             // → View
	add(k(5, "esc"))               // close

	// --- Sort options ---
	add(k(5, "s"))
	add(k(5, "esc"))

	// --- Help overlay: the full key reference ---
	add(k(5, "?"))
	add(k(7, "esc"))

	// --- Embedded vim-style editor ---
	add(k(4, "G"))                 // jump to README.md (last entry)
	add(k(8, "enter"))             // open it in the editor
	add(k(4, "o"))                 // open a new line below (insert mode)
	addAll(typeRunes(2, "edited in myconsole"))
	add(k(6, "esc"))               // back to normal mode
	add(k(6, ":", "q", "!", "enter")) // close without saving

	add(k(8))                      // final resting frame
	return b
}

func main() {
	dir := flag.String("dir", "", "directory to browse")
	out := flag.String("out", "../_demo/frames", "output directory for .ansi frames")
	theme := flag.String("theme", "default", "theme name")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "usage: film -dir <path> [-out dir] [-theme name]")
		os.Exit(1)
	}

	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	cfg := config.Default()
	cfg.Theme.Name = *theme
	bridge := icloud.NewBridge()
	queue := icloud.NewQueue(bridge, cfg.ICloud.MaxConcurrentDownloads, "")

	var m tea.Model = ui.New(cfg, *dir, bridge, queue)
	m = apply(m, tea.WindowSizeMsg{Width: cols, Height: rows})
	m = run(m, m.Init())

	frame := 0
	emit := func(n int) {
		view := m.View()
		for i := 0; i < n; i++ {
			path := filepath.Join(*out, fmt.Sprintf("f%04d.ansi", frame))
			if err := os.WriteFile(path, []byte(view), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			frame++
		}
	}

	for _, b := range storyboard() {
		for _, key := range b.keys {
			m = apply(m, keyMsg(key))
		}
		hold := b.hold
		if hold < 1 {
			hold = 1
		}
		emit(hold)
	}
	fmt.Printf("wrote %d frames to %s\n", frame, *out)
}

// apply feeds one message and then chases the resulting commands.
func apply(m tea.Model, msg tea.Msg) tea.Model {
	m, cmd := m.Update(msg)
	return run(m, cmd)
}

// run executes a command tree synchronously, skipping sleeping timers.
func run(m tea.Model, cmd tea.Cmd) tea.Model {
	if cmd == nil {
		return m
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if msg == nil {
			return m
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				m = run(m, c)
			}
			return m
		}
		return apply(m, msg)
	case <-time.After(400 * time.Millisecond):
		return m
	}
}

func keyMsg(key string) tea.KeyMsg {
	switch key {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "F3":
		return tea.KeyMsg{Type: tea.KeyF3}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}
