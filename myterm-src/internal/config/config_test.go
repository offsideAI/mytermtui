package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.General.StartDir != "~" || !cfg.General.ConfirmTrash || !cfg.General.DirsFirst {
		t.Errorf("defaults = %+v", cfg.General)
	}
	if cfg.ICloud.MaxConcurrentDownloads != 3 || cfg.ICloud.PollIntervalMs != 500 {
		t.Errorf("icloud defaults = %+v", cfg.ICloud)
	}
}

func TestLoadOverlay(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(p, []byte(`
[general]
start_dir = "/tmp"
show_hidden = true

[icloud]
max_concurrent_downloads = 7

[theme]
name = "dracula"

[keys]
quit = ["ctrl+x"]
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.General.StartDir != "/tmp" || !cfg.General.ShowHidden {
		t.Errorf("general = %+v", cfg.General)
	}
	if !cfg.General.ConfirmTrash {
		t.Error("unset field should keep default true")
	}
	if cfg.ICloud.MaxConcurrentDownloads != 7 || cfg.ICloud.PollIntervalMs != 500 {
		t.Errorf("icloud = %+v", cfg.ICloud)
	}
	if cfg.Theme.Name != "dracula" {
		t.Errorf("theme = %q", cfg.Theme.Name)
	}
	if len(cfg.Keys["quit"]) != 1 || cfg.Keys["quit"][0] != "ctrl+x" {
		t.Errorf("keys = %v", cfg.Keys)
	}
}

func TestLoadBadTOMLFallsBack(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("not [valid toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err == nil {
		t.Fatal("want parse error")
	}
	if cfg.General.StartDir != "~" {
		t.Error("bad config should fall back to defaults")
	}
}

func TestClampBadValues(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("[icloud]\nmax_concurrent_downloads = 0\npoll_interval_ms = 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ICloud.MaxConcurrentDownloads != 1 || cfg.ICloud.PollIntervalMs != 500 {
		t.Errorf("clamped = %+v", cfg.ICloud)
	}
}

func TestExpandTilde(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := ExpandTilde("~/x"); got != filepath.Join(home, "x") {
		t.Errorf("ExpandTilde(~/x) = %q", got)
	}
	if got := ExpandTilde("/abs"); got != "/abs" {
		t.Errorf("ExpandTilde(/abs) = %q", got)
	}
	if got := ExpandTilde("~"); got != home {
		t.Errorf("ExpandTilde(~) = %q", got)
	}
}
