// Package config loads mydb's optional TOML configuration and provides
// the default paths.
package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type General struct {
	ShowHints bool `toml:"show_hints"` // nano-style shortcut bar
	ShowPanel bool `toml:"show_panel"` // right workspace panel on launch
	// SplitRatio is the tree's share of the width when the workspace
	// panel is open (0.15–0.85).
	SplitRatio float64 `toml:"split_ratio"`
	// ConfirmMedium gates y/n confirmations on Medium-danger operations
	// (CREATE/ALTER/GRANT…). High-danger confirmations cannot be disabled.
	ConfirmMedium bool `toml:"confirm_medium"`
}

type Query struct {
	PageSize     int `toml:"page_size"`     // data-grid page
	MaxRows      int `toml:"max_rows"`      // result-set cap per execution
	HistoryLimit int `toml:"history_limit"` // query_history retention
}

type Backup struct {
	Dir          string `toml:"dir"`
	PreferPgDump bool   `toml:"prefer_pg_dump"`
}

type ThemeCfg struct {
	Name string `toml:"name"`
}

type Config struct {
	General General             `toml:"general"`
	Query   Query               `toml:"query"`
	Backup  Backup              `toml:"backup"`
	Theme   ThemeCfg            `toml:"theme"`
	Keys    map[string][]string `toml:"keys"`
}

func Default() Config {
	return Config{
		General: General{
			ShowHints:     true,
			ShowPanel:     true,
			SplitRatio:    0.35,
			ConfirmMedium: true,
		},
		Query: Query{
			PageSize:     500,
			MaxRows:      10000,
			HistoryLimit: 1000,
		},
		Backup: Backup{
			Dir:          "~/Backups/mydb",
			PreferPgDump: true,
		},
		Theme: ThemeCfg{Name: "default"},
		Keys:  map[string][]string{},
	}
}

// DefaultPath is ~/.config/mydb/config.toml (per spec; not
// os.UserConfigDir, which is ~/Library/Application Support on macOS).
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "mydb", "config.toml")
}

// StateDir holds runtime state (the master registry database).
func StateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	return filepath.Join(home, ".local", "state", "mydb")
}

// Load returns defaults overlaid with the TOML file at path, if present.
// A missing file is not an error.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Default(), err
	}
	if cfg.General.SplitRatio < 0.15 || cfg.General.SplitRatio > 0.85 {
		cfg.General.SplitRatio = 0.35
	}
	if cfg.Query.PageSize < 10 {
		cfg.Query.PageSize = 500
	}
	if cfg.Query.MaxRows < cfg.Query.PageSize {
		cfg.Query.MaxRows = cfg.Query.PageSize
	}
	if cfg.Query.HistoryLimit < 0 {
		cfg.Query.HistoryLimit = 1000
	}
	return cfg, nil
}

// ExpandTilde expands a leading ~ to the user's home directory.
func ExpandTilde(p string) string {
	if p == "~" || len(p) >= 2 && p[:2] == "~/" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}
