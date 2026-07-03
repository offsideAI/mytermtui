// Command screenshot renders mytermtui frames headlessly for docs.
//
// It drives the real ui.Model with scripted key scenes (no PTY, no
// running app) and writes each resulting frame as true-color ANSI text.
// scripts/ansi2png.py turns those frames into PNGs (from the repo root):
//
//	go -C mytermtui-src run ./cmd/screenshot -dir "<start dir>" -out ../screenshots/ansi
//	python3 scripts/ansi2png.py screenshots/ansi screenshots
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

	"github.com/offsideai/mytermtui/internal/config"
	"github.com/offsideai/mytermtui/internal/icloud"
	"github.com/offsideai/mytermtui/internal/ui"
)

const (
	cols = 120
	rows = 34
)

type scene struct {
	name  string
	theme string
	keys  []string // key names; "F3", "enter", "space", runes, …
}

// buildScenes returns the shot list. filter is typed into the filter bar
// for the filter/preview/get-info scenes — pick a string matching files
// you are happy to publish.
func buildScenes(filter string) []scene {
	typeFilter := []string{"f"}
	for _, r := range filter {
		typeFilter = append(typeFilter, string(r))
	}
	typeFilter = append(typeFilter, "enter")
	return []scene{
		{name: "01-browser", theme: "default",
			keys: []string{"j", "j", "j", "j", "j", "j", "j", "j", "space", "space", "up"}},
		{name: "02-filter", theme: "default", keys: typeFilter},
		{name: "03-preview", theme: "default",
			keys: append(append([]string{}, typeFilter...), "F3")},
		{name: "04-file-menu", theme: "default", keys: []string{"m"}},
		{name: "05-get-info", theme: "default",
			keys: append(append([]string{}, typeFilter...), "esc", "I")},
		{name: "06-help", theme: "default", keys: []string{"?"}},
		{name: "07-theme-dracula", theme: "dracula", keys: []string{"j", "j", "space", "j"}},
	}
}

func main() {
	dir := flag.String("dir", "", "directory to browse in the scenes")
	out := flag.String("out", "screenshots/ansi", "output directory for .ansi frames")
	filter := flag.String("filter", "walk", "query typed into the filter scenes")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "usage: screenshot -dir <path> [-out dir]")
		os.Exit(1)
	}

	// Deterministic styling regardless of the invoking terminal.
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	for _, sc := range buildScenes(*filter) {
		frame := renderScene(sc, *dir)
		path := filepath.Join(*out, sc.name+".ansi")
		if err := os.WriteFile(path, []byte(frame), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("wrote", path)
	}
}

func renderScene(sc scene, dir string) string {
	cfg := config.Default()
	cfg.Theme.Name = sc.theme
	bridge := icloud.NewBridge()
	queue := icloud.NewQueue(bridge, cfg.ICloud.MaxConcurrentDownloads, "")

	var m tea.Model = ui.New(cfg, dir, bridge, queue)
	m = apply(m, tea.WindowSizeMsg{Width: cols, Height: rows})
	m = run(m, m.Init())
	for _, k := range sc.keys {
		m = apply(m, keyMsg(k))
	}
	return m.View()
}

// apply feeds one message and then chases the resulting commands.
func apply(m tea.Model, msg tea.Msg) tea.Model {
	m, cmd := m.Update(msg)
	return run(m, cmd)
}

// run executes a command tree synchronously, skipping anything that does
// not resolve quickly (timers like status-note expiry or spinner ticks).
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
	case <-time.After(500 * time.Millisecond):
		return m // a sleeping tick; irrelevant for a still frame
	}
}

func keyMsg(k string) tea.KeyMsg {
	switch k {
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
	case "F3":
		return tea.KeyMsg{Type: tea.KeyF3}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}
