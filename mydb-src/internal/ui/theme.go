package ui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/offsideai/mydb/internal/dbx"
)

// Theme bundles every style the views use. Colors adapt to light/dark
// terminal backgrounds via lipgloss.AdaptiveColor where sensible.
type Theme struct {
	Name string

	MenuBar     lipgloss.Style
	MenuTitle   lipgloss.Style
	MenuTitleOn lipgloss.Style
	MenuItem    lipgloss.Style
	MenuItemOn  lipgloss.Style
	MenuKey     lipgloss.Style
	MenuBox     lipgloss.Style

	Breadcrumb lipgloss.Style
	Header     lipgloss.Style

	Row      lipgloss.Style
	Cursor   lipgloss.Style
	Selected lipgloss.Style
	Dim      lipgloss.Style
	Section  lipgloss.Style
	ConnRow  lipgloss.Style
	Table    lipgloss.Style
	View     lipgloss.Style
	Index    lipgloss.Style
	Column   lipgloss.Style
	Role     lipgloss.Style
	Danger   lipgloss.Style

	GlyphOpen   lipgloss.Style // ● connected
	GlyphClosed lipgloss.Style // ○ saved
	GlyphBusy   lipgloss.Style // ◐ connecting
	GlyphFailed lipgloss.Style // ✗ connect error

	StatusBar  lipgloss.Style
	StatusInfo lipgloss.Style
	StatusOK   lipgloss.Style
	StatusWarn lipgloss.Style
	StatusErr  lipgloss.Style

	BarFill lipgloss.Style

	ModalBox   lipgloss.Style
	ModalTitle lipgloss.Style
	ModalDim   lipgloss.Style

	PanelTitle lipgloss.Style
	PanelMeta  lipgloss.Style
}

type palette struct {
	accent  lipgloss.TerminalColor // selection / highlights
	surface lipgloss.TerminalColor // bars background
	text    lipgloss.TerminalColor // text on surface
	dim     lipgloss.TerminalColor
	blue    lipgloss.TerminalColor
	green   lipgloss.TerminalColor
	yellow  lipgloss.TerminalColor
	red     lipgloss.TerminalColor
	magenta lipgloss.TerminalColor
	cyan    lipgloss.TerminalColor
	border  lipgloss.TerminalColor
}

func palettes(name string) palette {
	switch name {
	case "dracula":
		return palette{
			accent:  lipgloss.Color("#bd93f9"),
			surface: lipgloss.Color("#44475a"),
			text:    lipgloss.Color("#f8f8f2"),
			dim:     lipgloss.Color("#6272a4"),
			blue:    lipgloss.Color("#8be9fd"),
			green:   lipgloss.Color("#50fa7b"),
			yellow:  lipgloss.Color("#f1fa8c"),
			red:     lipgloss.Color("#ff5555"),
			magenta: lipgloss.Color("#ff79c6"),
			cyan:    lipgloss.Color("#8be9fd"),
			border:  lipgloss.Color("#6272a4"),
		}
	case "solarized":
		return palette{
			accent:  lipgloss.Color("#268bd2"),
			surface: lipgloss.AdaptiveColor{Light: "#eee8d5", Dark: "#073642"},
			text:    lipgloss.AdaptiveColor{Light: "#586e75", Dark: "#93a1a1"},
			dim:     lipgloss.AdaptiveColor{Light: "#93a1a1", Dark: "#586e75"},
			blue:    lipgloss.Color("#268bd2"),
			green:   lipgloss.Color("#859900"),
			yellow:  lipgloss.Color("#b58900"),
			red:     lipgloss.Color("#dc322f"),
			magenta: lipgloss.Color("#d33682"),
			cyan:    lipgloss.Color("#2aa198"),
			border:  lipgloss.AdaptiveColor{Light: "#93a1a1", Dark: "#586e75"},
		}
	default: // "default": lean on the terminal's own ANSI palette
		return palette{
			accent:  lipgloss.Color("12"),
			surface: lipgloss.AdaptiveColor{Light: "254", Dark: "236"},
			text:    lipgloss.AdaptiveColor{Light: "235", Dark: "252"},
			dim:     lipgloss.Color("8"),
			blue:    lipgloss.Color("4"),
			green:   lipgloss.Color("2"),
			yellow:  lipgloss.Color("3"),
			red:     lipgloss.Color("1"),
			magenta: lipgloss.Color("5"),
			cyan:    lipgloss.Color("6"),
			border:  lipgloss.Color("8"),
		}
	}
}

// NewTheme builds the style set for a named theme ("default", "dracula",
// "solarized"). Unknown names fall back to "default".
func NewTheme(name string) Theme {
	p := palettes(name)
	t := Theme{Name: name}

	t.MenuBar = lipgloss.NewStyle().Background(p.surface).Foreground(p.text)
	t.MenuTitle = t.MenuBar.Padding(0, 1)
	t.MenuTitleOn = lipgloss.NewStyle().Background(p.accent).Foreground(lipgloss.Color("0")).Padding(0, 1).Bold(true)
	t.MenuItem = lipgloss.NewStyle().Foreground(p.text)
	t.MenuItemOn = lipgloss.NewStyle().Background(p.accent).Foreground(lipgloss.Color("0")).Bold(true)
	t.MenuKey = lipgloss.NewStyle().Foreground(p.dim)
	t.MenuBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.border).Padding(0, 1)

	t.Breadcrumb = lipgloss.NewStyle().Foreground(p.blue).Bold(true)
	t.Header = lipgloss.NewStyle().Foreground(p.dim).Underline(true)

	t.Row = lipgloss.NewStyle()
	t.Cursor = lipgloss.NewStyle().Reverse(true).Bold(true)
	t.Selected = lipgloss.NewStyle().Background(p.surface)
	t.Dim = lipgloss.NewStyle().Foreground(p.dim)
	t.Section = lipgloss.NewStyle().Foreground(p.accent).Bold(true)
	t.ConnRow = lipgloss.NewStyle().Foreground(p.blue).Bold(true)
	t.Table = lipgloss.NewStyle().Foreground(p.cyan)
	t.View = lipgloss.NewStyle().Foreground(p.green)
	t.Index = lipgloss.NewStyle().Foreground(p.dim)
	t.Column = lipgloss.NewStyle()
	t.Role = lipgloss.NewStyle().Foreground(p.magenta)
	t.Danger = lipgloss.NewStyle().Foreground(p.red).Bold(true)

	t.GlyphOpen = lipgloss.NewStyle().Foreground(p.green)
	t.GlyphClosed = lipgloss.NewStyle().Foreground(p.dim)
	t.GlyphBusy = lipgloss.NewStyle().Foreground(p.yellow).Bold(true)
	t.GlyphFailed = lipgloss.NewStyle().Foreground(p.red).Bold(true)

	t.StatusBar = lipgloss.NewStyle().Background(p.surface).Foreground(p.text)
	t.StatusInfo = lipgloss.NewStyle().Background(p.surface).Foreground(p.text)
	t.StatusOK = lipgloss.NewStyle().Background(p.surface).Foreground(p.green).Bold(true)
	t.StatusWarn = lipgloss.NewStyle().Background(p.surface).Foreground(p.yellow).Bold(true)
	t.StatusErr = lipgloss.NewStyle().Background(p.surface).Foreground(p.red).Bold(true)

	t.BarFill = lipgloss.NewStyle().Foreground(p.accent)

	t.ModalBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.accent).Padding(0, 2)
	t.ModalTitle = lipgloss.NewStyle().Foreground(p.accent).Bold(true)
	t.ModalDim = lipgloss.NewStyle().Foreground(p.dim)

	t.PanelTitle = lipgloss.NewStyle().Foreground(p.accent).Bold(true)
	t.PanelMeta = lipgloss.NewStyle().Foreground(p.dim)

	return t
}

// nodeStyle maps a tree node's kind to its row style.
func (t Theme) nodeStyle(n dbx.Node) lipgloss.Style {
	switch n.Kind {
	case dbx.KSection:
		return t.Section
	case dbx.KConnection:
		return t.ConnRow
	case dbx.KGroup:
		return t.Dim
	case dbx.KTable, dbx.KDatabase, dbx.KSchema:
		return t.Table
	case dbx.KView:
		return t.View
	case dbx.KIndex:
		return t.Index
	case dbx.KColumn:
		return t.Column
	case dbx.KRole:
		return t.Role
	}
	return t.Row
}
