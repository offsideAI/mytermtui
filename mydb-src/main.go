// mydb: a keyboard-driven TUI for browsing and administering local and
// remote databases (SQLite and PostgreSQL).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/offsideai/mydb/internal/config"
	"github.com/offsideai/mydb/internal/registry"
	"github.com/offsideai/mydb/internal/ui"
)

const version = "0.1.0"

func main() {
	cfgPath := flag.String("config", config.DefaultPath(), "path to config.toml")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: mydb [flags]\n\n")
		fmt.Fprintf(os.Stderr, "A keyboard-driven database browser and admin console.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println("mydb", version)
		return
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mydb: config %s: %v (using defaults)\n", *cfgPath, err)
	}

	reg, err := registry.Open(filepath.Join(config.StateDir(), "registry.db"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "mydb: registry: %v\n", err)
		os.Exit(1)
	}
	defer reg.Close()

	m := ui.New(cfg, reg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "mydb: %v\n", err)
		os.Exit(1)
	}
}
