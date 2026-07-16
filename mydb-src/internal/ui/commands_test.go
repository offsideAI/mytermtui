package ui

import (
	"strings"
	"testing"

	"github.com/offsideai/mydb/internal/dbx/fake"
	"github.com/offsideai/mydb/internal/registry"
)

// fakeConnFor digs the fake connection handle out of the model.
func fakeConnFor(t *testing.T, m *Model, name string) *fake.Conn {
	t.Helper()
	for _, c := range m.conns {
		if c.Name == name {
			fc, ok := m.open[c.ID].(*fake.Conn)
			if !ok {
				t.Fatalf("%s is not open on the fake driver", name)
			}
			return fc
		}
	}
	t.Fatalf("connection %s not found", name)
	return nil
}

func TestTemplateFlowCreateUser(t *testing.T) {
	m, _ := fixture(t)
	fakePGFixture(t, m, "pgc", "appdb")
	press(t, m, "enter") // connect

	press(t, m, "C")
	if _, ok := m.modal.(*TemplatePicker); !ok {
		t.Fatalf("C should open the template picker, modal=%#v", m.modal)
	}
	press(t, m, "enter") // create user…
	if _, ok := m.modal.(*templateFlow); !ok {
		t.Fatalf("expected the parameter flow, modal=%#v", m.modal)
	}
	for _, r := range "alice" {
		press(t, m, string(r))
	}
	press(t, m, "enter") // → password param
	for _, r := range "p'w" {
		press(t, m, string(r))
	}
	press(t, m, "enter") // → preview
	tf := m.modal.(*templateFlow)
	if tf.preview == nil {
		t.Fatal("flow should be in the preview stage")
	}
	press(t, m, "enter") // → Medium confirm
	if _, ok := m.modal.(*ConfirmModal); !ok {
		t.Fatalf("Medium plan must confirm, modal=%#v", m.modal)
	}
	press(t, m, "y") // run

	want := `CREATE USER "alice" WITH PASSWORD 'p''w';`
	if got := fakeConnFor(t, m, "pgc").LastSQL; got != want {
		t.Fatalf("executed SQL = %q, want %q", got, want)
	}
	// The fake echoes a row, so the read-back InfoModal opens.
	if _, ok := m.modal.(*InfoModal); !ok {
		t.Fatalf("read-back rows should show, modal=%#v", m.modal)
	}
	press(t, m, "esc")
}

func TestTemplateHighRequiresTypedToken(t *testing.T) {
	m, _ := fixture(t)
	fakePGFixture(t, m, "pgc", "appdb")
	press(t, m, "enter")

	press(t, m, "C")
	tp := m.modal.(*TemplatePicker)
	for tp.templates[tp.cursor].Key != "drop-user" {
		press(t, m, "down")
	}
	press(t, m, "enter")
	for _, r := range "bob" {
		press(t, m, string(r))
	}
	press(t, m, "enter") // preview
	press(t, m, "enter") // → typed confirm
	tc, ok := m.modal.(*TypedConfirmModal)
	if !ok {
		t.Fatalf("High plan must use the typed confirm, modal=%#v", m.modal)
	}
	if tc.Token != "bob" {
		t.Fatalf("token = %q", tc.Token)
	}
	press(t, m, "enter") // nothing typed → stays open
	if m.modal == nil {
		t.Fatal("empty confirmation must not run a High plan")
	}
	press(t, m, "b", "o", "b", "enter")
	if got := fakeConnFor(t, m, "pgc").LastSQL; got != `DROP USER "bob";` {
		t.Fatalf("executed SQL = %q", got)
	}
}

func TestMaintenanceSqliteVacuumAndIntegrity(t *testing.T) {
	m, _ := fixture(t)
	press(t, m, "down", "enter") // connect the sqlite fixture

	press(t, m, "M")
	mp, ok := m.modal.(*MaintPicker)
	if !ok {
		t.Fatalf("M should open the maintenance picker, modal=%#v", m.modal)
	}
	if mp.ops[0].Label != "VACUUM" {
		t.Fatalf("ops: %+v", mp.ops)
	}
	press(t, m, "enter") // VACUUM → Medium confirm
	if _, ok := m.modal.(*ConfirmModal); !ok {
		t.Fatalf("VACUUM must confirm, modal=%#v", m.modal)
	}
	press(t, m, "y") // runs against the real file
	if m.modal != nil {
		t.Fatalf("VACUUM returns no rows; modal=%#v", m.modal)
	}
	if !strings.Contains(m.status.text, "done") {
		t.Fatalf("status = %q", m.status.text)
	}

	// Integrity check is DangerNone: runs immediately, shows its rows.
	press(t, m, "M", "down", "down", "down", "enter")
	im, ok := m.modal.(*InfoModal)
	if !ok {
		t.Fatalf("integrity check should show results, modal=%#v", m.modal)
	}
	if !strings.Contains(strings.Join(im.Lines, "\n"), "ok") {
		t.Fatalf("integrity result: %v", im.Lines)
	}
	press(t, m, "esc")
}

func TestReadOnlyBlocksPlansAndShowsGlyph(t *testing.T) {
	m, dbPath := fixture(t)
	if _, err := m.reg.Create(registry.Connection{
		Name: "ro", Engine: "sqlite", Locality: "local", Path: dbPath,
		Options: registry.OptionsJSON(true),
	}); err != nil {
		t.Fatal(err)
	}
	press(t, m, "ctrl+r") // reload the registry listing
	m.cursor = indexOf2(t, m, "ro")
	press(t, m, "enter") // connect read-only

	if !strings.Contains(m.View(), "⏸") {
		t.Fatal("read-only connection should show the ⏸ glyph")
	}
	press(t, m, "M")
	press(t, m, "enter") // VACUUM → blocked before any confirm
	if m.modal != nil {
		t.Fatalf("read-only plan must not reach a confirm, modal=%#v", m.modal)
	}
	if !strings.Contains(m.status.text, "read-only") {
		t.Fatalf("status = %q", m.status.text)
	}
}
