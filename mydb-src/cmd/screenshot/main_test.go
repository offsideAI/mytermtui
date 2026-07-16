package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Light golden assertions: every scene renders and shows its key content.
func TestScenesRender(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)

	markers := map[string][]string{
		"01-browser":   {"Local", "users", "Tables"},
		"02-data-grid": {"user01@example.com", "rows 1–12"},
		"03-sql":       {"user01@example.com", "3 row(s)"},
		"04-annex":     {"public", "Roles", "sibling_a", "Local (Annex)"},
		"05-conn-form": {"New connection", "URL", "Engine"},
		"06-commands":  {"Common commands", "Create user", "Create database"},
	}
	for _, sc := range buildScenes() {
		frame, err := renderScene(sc)
		if err != nil {
			t.Fatalf("%s: %v", sc.name, err)
		}
		for _, want := range markers[sc.name] {
			if !strings.Contains(frame, want) {
				t.Errorf("%s: frame missing %q", sc.name, want)
			}
		}
	}
}
