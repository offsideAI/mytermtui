// Package config loads myterm's optional TOML configuration and
// provides the default key bindings and paths.
package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type General struct {
	StartDir     string `toml:"start_dir"`
	ShowHidden   bool   `toml:"show_hidden"`
	ConfirmTrash bool   `toml:"confirm_trash"`
	DirsFirst    bool   `toml:"dirs_first"`
	ShowHints    bool   `toml:"show_hints"`   // nano-style shortcut bar
	ShowPreview  bool   `toml:"show_preview"` // right detail panel on launch
	// SplitRatio is the left panel's share of the width when the
	// dual-panel view is open (0.15–0.85).
	SplitRatio float64 `toml:"split_ratio"`
	// EnterOpensFile controls what enter does on a file: "reveal" shows
	// it in its enclosing folder in Finder (default); "app" opens it in
	// its default application (the o key always does that regardless).
	EnterOpensFile string `toml:"enter_opens_file"`
}

type ICloud struct {
	MaxConcurrentDownloads int `toml:"max_concurrent_downloads"`
	PollIntervalMs         int `toml:"poll_interval_ms"`
}

type ThemeCfg struct {
	Name string `toml:"name"`
}

type Config struct {
	General General             `toml:"general"`
	ICloud  ICloud              `toml:"icloud"`
	Theme   ThemeCfg            `toml:"theme"`
	Keys    map[string][]string `toml:"keys"`
}

func Default() Config {
	return Config{
		General: General{
			StartDir:       "~",
			ShowHidden:     false,
			ConfirmTrash:   true,
			DirsFirst:      true,
			ShowHints:      true,
			ShowPreview:    true,
			SplitRatio:     0.30,
			EnterOpensFile: "reveal",
		},
		ICloud: ICloud{
			MaxConcurrentDownloads: 3,
			PollIntervalMs:         500,
		},
		Theme: ThemeCfg{Name: "default"},
		Keys:  map[string][]string{},
	}
}

// DefaultPath is ~/.config/myterm/config.toml (per spec; not
// os.UserConfigDir, which is ~/Library/Application Support on macOS).
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "myterm", "config.toml")
}

// StateDir holds runtime state (download queue persistence).
func StateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	return filepath.Join(home, ".local", "state", "myterm")
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
	if cfg.ICloud.MaxConcurrentDownloads < 1 {
		cfg.ICloud.MaxConcurrentDownloads = 1
	}
	if cfg.ICloud.PollIntervalMs < 100 {
		cfg.ICloud.PollIntervalMs = 500
	}
	if cfg.General.EnterOpensFile != "app" {
		cfg.General.EnterOpensFile = "reveal"
	}
	if cfg.General.SplitRatio < 0.15 || cfg.General.SplitRatio > 0.85 {
		cfg.General.SplitRatio = 0.30
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
