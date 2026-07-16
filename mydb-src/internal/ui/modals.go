package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Modal is a centered overlay that captures all key input while open.
// Update returns the modal to keep (nil closes it).
type Modal interface {
	Update(m *Model, msg tea.KeyMsg) (Modal, tea.Cmd)
	View(m *Model, width int) string
}

// --- confirm (y/n) -------------------------------------------------------

type ConfirmModal struct {
	Title    string
	Body     []string
	YesLabel string
	Yes      func(m *Model) tea.Cmd
}

func (c *ConfirmModal) Update(m *Model, msg tea.KeyMsg) (Modal, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		return nil, c.Yes(m)
	case "n", "N", "esc", "q":
		return nil, nil
	}
	return c, nil
}

func (c *ConfirmModal) View(m *Model, width int) string {
	t := m.theme
	yes := c.YesLabel
	if yes == "" {
		yes = "confirm"
	}
	lines := []string{t.ModalTitle.Render(c.Title), ""}
	lines = append(lines, c.Body...)
	lines = append(lines, "", t.ModalDim.Render("y/enter ")+yes+t.ModalDim.Render("  ·  n/esc cancel"))
	return strings.Join(lines, "\n")
}

// --- typed confirm (High danger: type the object's name) ------------------

type TypedConfirmModal struct {
	Title string
	Body  []string
	Token string // the exact text the user must type
	Yes   func(m *Model) tea.Cmd
	input textinput.Model
}

func newTypedConfirm(title string, body []string, token string, yes func(m *Model) tea.Cmd) *TypedConfirmModal {
	ti := textinput.New()
	ti.Placeholder = token
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 44
	return &TypedConfirmModal{Title: title, Body: body, Token: token, Yes: yes, input: ti}
}

func (c *TypedConfirmModal) Update(m *Model, msg tea.KeyMsg) (Modal, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return nil, nil
	case "enter":
		if c.input.Value() == c.Token {
			return nil, c.Yes(m)
		}
		return c, nil // not typed yet — stay open
	}
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	return c, cmd
}

func (c *TypedConfirmModal) View(m *Model, width int) string {
	t := m.theme
	lines := []string{t.Danger.Render(c.Title), ""}
	lines = append(lines, c.Body...)
	lines = append(lines, "",
		"Type "+t.ModalTitle.Render(c.Token)+" to confirm:",
		c.input.View(), "",
		t.ModalDim.Render("enter confirm · esc cancel"))
	return strings.Join(lines, "\n")
}

// --- input (single-line prompt) --------------------------------------------

type InputModal struct {
	Title    string
	Input    textinput.Model
	OnSubmit func(m *Model, value string) tea.Cmd
}

func (im *InputModal) Update(m *Model, msg tea.KeyMsg) (Modal, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return nil, nil
	case "enter":
		val := strings.TrimSpace(im.Input.Value())
		if val == "" {
			return nil, nil
		}
		return nil, im.OnSubmit(m, val)
	}
	var cmd tea.Cmd
	im.Input, cmd = im.Input.Update(msg)
	return im, cmd
}

func (im *InputModal) View(m *Model, width int) string {
	t := m.theme
	return strings.Join([]string{
		t.ModalTitle.Render(im.Title), "",
		im.Input.View(), "",
		t.ModalDim.Render("enter confirm · esc cancel"),
	}, "\n")
}

// --- help -----------------------------------------------------------------

type HelpModal struct{ scroll int }

func (h *HelpModal) lines(m *Model) []string {
	t := m.theme
	var out []string
	for _, sec := range helpSections {
		out = append(out, t.ModalTitle.Render(sec.Title))
		for _, act := range sec.Actions {
			keys := m.keys.byAction[act]
			shown := make([]string, 0, len(keys))
			for _, k := range keys {
				if k == " " {
					k = "space"
				}
				shown = append(shown, k)
			}
			out = append(out, "  "+pad(strings.Join(shown, " / "), 22)+t.ModalDim.Render(actionHelp[act]))
		}
		out = append(out, "")
	}
	return out
}

func (h *HelpModal) Update(m *Model, msg tea.KeyMsg) (Modal, tea.Cmd) {
	total := len(h.lines(m))
	page := m.listHeight() - 4
	if page < 3 {
		page = 3
	}
	switch msg.String() {
	case "esc", "q", "?", "f1", "enter":
		return nil, nil
	case "up", "k":
		h.scroll--
	case "down", "j":
		h.scroll++
	case "pgup":
		h.scroll -= page
	case "pgdown":
		h.scroll += page
	case "g":
		h.scroll = 0
	case "G":
		h.scroll = total
	}
	if h.scroll > total-page {
		h.scroll = total - page
	}
	if h.scroll < 0 {
		h.scroll = 0
	}
	return h, nil
}

func (h *HelpModal) View(m *Model, width int) string {
	lines := h.lines(m)
	page := m.listHeight() - 4
	if page < 3 {
		page = 3
	}
	end := h.scroll + page
	if end > len(lines) {
		end = len(lines)
	}
	body := strings.Join(lines[h.scroll:end], "\n")
	footer := m.theme.ModalDim.Render(fmt.Sprintf("j/k scroll (%d–%d of %d) · esc close", h.scroll+1, end, len(lines)))
	return m.theme.ModalTitle.Render("mydb — keyboard reference") + "\n\n" + body + "\n" + footer
}

// --- info (scrollable text) -------------------------------------------------

type InfoModal struct {
	Title  string
	Lines  []string
	scroll int
}

func (im *InfoModal) Update(m *Model, msg tea.KeyMsg) (Modal, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "enter":
		return nil, nil
	case "up", "k":
		if im.scroll > 0 {
			im.scroll--
		}
	case "down", "j":
		if im.scroll < len(im.Lines)-1 {
			im.scroll++
		}
	}
	return im, nil
}

func (im *InfoModal) View(m *Model, width int) string {
	page := m.listHeight() - 4
	if page < 3 {
		page = 3
	}
	end := im.scroll + page
	if end > len(im.Lines) {
		end = len(im.Lines)
	}
	body := strings.Join(im.Lines[im.scroll:end], "\n")
	return m.theme.ModalTitle.Render(im.Title) + "\n\n" + body + "\n\n" + m.theme.ModalDim.Render("esc close")
}
