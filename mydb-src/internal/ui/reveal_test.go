package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"

	"github.com/offsideai/mydb/internal/registry"
)

func TestFormPasswordRevealToggle(t *testing.T) {
	m, _ := fixture(t)
	press(t, m, "B")
	f, ok := m.modal.(*connForm)
	if !ok {
		t.Fatal("B should open the connection form")
	}
	for f.fieldOrder()[f.focus] != "engine" {
		press(t, m, "down")
	}
	press(t, m, " ") // cycle sqlite → postgres
	if engines[f.engine] != "postgres" {
		t.Fatalf("engine should be postgres, is %s", engines[f.engine])
	}
	// name(0) → … → password(7)
	for f.fieldOrder()[f.focus] != "password" {
		press(t, m, "down")
	}
	pw := f.fields["password"]
	if pw.EchoMode != textinput.EchoPassword {
		t.Fatal("password field must open masked")
	}
	press(t, m, "ctrl+r")
	if pw.EchoMode != textinput.EchoNormal {
		t.Fatal("ctrl+r should reveal the password field")
	}
	press(t, m, "ctrl+r")
	if pw.EchoMode != textinput.EchoPassword {
		t.Fatal("second ctrl+r should re-mask")
	}
	press(t, m, "esc")
}

// pgFixtureConn saves an (unconnected) postgres connection with a secret
// and reloads the tree; returns after placing the cursor on it.
func pgFixtureConn(t *testing.T, m *Model) {
	t.Helper()
	if _, err := m.reg.Create(registry.Connection{
		Name: "remote-pg", Engine: "postgres", Locality: "remote",
		Host: "db.example.com", Port: 5432, DBName: "appdb",
		Username: "admin", Secret: "s3cret!",
	}); err != nil {
		t.Fatal(err)
	}
	press(t, m, "ctrl+r") // refresh reloads the registry listing
	// rows: Local, app, Local (Annex), Remote, remote-pg
	press(t, m, "down", "down", "down", "down")
	if n := m.currentNode(); n == nil || n.Name != "remote-pg" {
		t.Fatalf("cursor should be on remote-pg, is on %v", m.currentNode())
	}
}

func panelText(m *Model) string {
	return strings.Join(m.infoPanel(60, 30), "\n")
}

func TestInfoPanelReveal(t *testing.T) {
	m, _ := fixture(t)
	pgFixtureConn(t, m)

	if got := panelText(m); strings.Contains(got, "s3cret!") || !strings.Contains(got, "••••") {
		t.Fatalf("panel must show a masked password by default:\n%s", got)
	}

	press(t, m, "p")
	if m.revealID == 0 {
		t.Fatal("p should reveal")
	}
	if got := panelText(m); !strings.Contains(got, "s3cret!") {
		t.Fatalf("panel should show the password while revealed:\n%s", got)
	}

	press(t, m, "p") // toggle off
	if m.revealID != 0 || strings.Contains(panelText(m), "s3cret!") {
		t.Fatal("second p should hide the password")
	}
}

func TestRevealAutoHideAndStaleTick(t *testing.T) {
	m, _ := fixture(t)
	pgFixtureConn(t, m)

	press(t, m, "p")
	seq := m.revealSeq
	apply(t, m, revealExpireMsg{seq: seq - 1}) // stale tick from an older reveal
	if m.revealID == 0 {
		t.Fatal("a stale tick must not hide the current reveal")
	}
	apply(t, m, revealExpireMsg{seq: seq})
	if m.revealID != 0 {
		t.Fatal("the matching tick should auto-hide")
	}
}

func TestRevealClearsOnDelete(t *testing.T) {
	m, _ := fixture(t)
	pgFixtureConn(t, m)

	press(t, m, "p")
	if m.revealID == 0 {
		t.Fatal("precondition: revealed")
	}
	press(t, m, "X")
	for _, r := range "remote-pg" {
		press(t, m, string(r))
	}
	press(t, m, "enter")
	if m.revealID != 0 {
		t.Fatal("deleting the connection must clear the reveal")
	}
}

func TestRevealWithoutSecretIsNoop(t *testing.T) {
	m, _ := fixture(t)
	press(t, m, "down") // the sqlite connection (no password)
	press(t, m, "p")
	if m.revealID != 0 {
		t.Fatal("connections without a saved password have nothing to reveal")
	}
}
