// mytermtui: a keyboard-driven filesystem browser and iCloud file syncer.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/offsideai/mytermtui/internal/config"
	"github.com/offsideai/mytermtui/internal/icloud"
	"github.com/offsideai/mytermtui/internal/ui"
)

const version = "0.1.0"

func main() {
	cfgPath := flag.String("config", config.DefaultPath(), "path to config.toml")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: mytermtui [flags] [start-dir]\n\n")
		fmt.Fprintf(os.Stderr, "A keyboard-driven file browser with iCloud Drive download/evict support.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println("mytermtui", version)
		return
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mytermtui: config %s: %v (using defaults)\n", *cfgPath, err)
	}

	startDir := config.ExpandTilde(cfg.General.StartDir)
	if flag.NArg() > 0 {
		startDir = config.ExpandTilde(flag.Arg(0))
	}
	if abs, err := filepath.Abs(startDir); err == nil {
		startDir = abs
	}
	if fi, err := os.Stat(startDir); err != nil || !fi.IsDir() {
		fmt.Fprintf(os.Stderr, "mytermtui: %s is not a browsable directory\n", startDir)
		os.Exit(1)
	}

	bridge := icloud.NewBridge()
	queue := icloud.NewQueue(bridge, cfg.ICloud.MaxConcurrentDownloads,
		filepath.Join(config.StateDir(), "queue.json"))
	queue.Restore()

	m := ui.New(cfg, startDir, bridge, queue)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "mytermtui: %v\n", err)
		os.Exit(1)
	}
}
