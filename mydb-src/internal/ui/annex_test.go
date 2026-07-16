package ui

import (
	"testing"

	"github.com/offsideai/mydb/internal/dbx"
	"github.com/offsideai/mydb/internal/dbx/fake"
	"github.com/offsideai/mydb/internal/registry"
)

// fakePGFixture saves a fake-postgres connection and installs the fake
// driver, leaving the cursor on the connection row.
func fakePGFixture(t *testing.T, m *Model, name, dbname string) {
	t.Helper()
	m.drivers["postgres"] = fake.New()
	if _, err := m.reg.Create(registry.Connection{
		Name: name, Engine: "postgres", Locality: "local",
		Host: "srv", Port: 5432, DBName: dbname, Username: "u",
	}); err != nil {
		t.Fatal(err)
	}
	press(t, m, "ctrl+r")
	m.cursor = indexOf2(t, m, name)
}

// indexOf2 finds a row by name (visible must contain it).
func indexOf2(t *testing.T, m *Model, name string) int {
	t.Helper()
	for vi, ni := range m.view {
		if m.nodes[ni].Name == name {
			return vi
		}
	}
	t.Fatalf("row %q not visible in %v", name, visible(m))
	return -1
}

func TestSavedConnScopedToOwnDatabase(t *testing.T) {
	m, _ := fixture(t)
	fakePGFixture(t, m, "pgc", "appdb")
	press(t, m, "enter") // connect + expand

	rows := visible(m)
	if !contains(rows, "public") || !contains(rows, "Roles") {
		t.Fatalf("connection should expand to its own schemas + Roles: %v", rows)
	}
	// The server's other databases never nest under the connection; the
	// connection's own database is not duplicated anywhere.
	if contains(rows, "appdb") {
		t.Fatalf("own database must not appear as a node: %v", rows)
	}
	for _, sib := range []string{"postgres", "sibling_a", "sibling_b"} {
		if !contains(rows, sib) {
			t.Fatalf("sibling %s should appear in the Annex: %v", sib, rows)
		}
	}
	// Siblings live under Local (Annex), i.e. deeper than the section and
	// AFTER it in display order.
	if indexOf2(t, m, "sibling_a") < indexOf2(t, m, "Local (Annex)") {
		t.Fatal("siblings must render inside Local (Annex)")
	}
}

func TestAnnexDatabaseBrowses(t *testing.T) {
	m, _ := fixture(t)
	fakePGFixture(t, m, "pgc", "appdb")
	press(t, m, "enter")

	m.cursor = indexOf2(t, m, "sibling_a")
	press(t, m, "enter") // expand the annex database → its schemas
	rows := visible(m)
	if !contains(rows, "public") {
		t.Fatalf("annex database should expand to schemas: %v", rows)
	}
	// The schema under sibling_a carries sibling_a's database ref.
	si := indexOf2(t, m, "sibling_a")
	schema := m.nodes[m.view[si+1]]
	if schema.Kind != dbx.KSchema || schema.Ref.Database != "sibling_a" {
		t.Fatalf("annex schema ref wrong: %+v", schema)
	}
}

func TestAnnexDedupesAndExcludesSaved(t *testing.T) {
	m, _ := fixture(t)
	fakePGFixture(t, m, "pgc", "appdb")
	press(t, m, "enter") // connect pgc

	// A second saved connection to the same server's "postgres" database:
	// that database must vanish from the Annex.
	fakePGFixture(t, m, "pgc2", "postgres")
	press(t, m, "enter") // connect pgc2 (also discovers)

	rows := visible(m)
	if contains(rows, "postgres") {
		t.Fatalf("a database with its own saved connection must leave the Annex: %v", rows)
	}
	// Both connections discovered sibling_a — it must appear exactly once.
	count := 0
	for _, r := range rows {
		if r == "sibling_a" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("sibling_a should be deduplicated, found %d: %v", count, rows)
	}
}

func TestAnnexClearsOnDisconnect(t *testing.T) {
	m, _ := fixture(t)
	fakePGFixture(t, m, "pgc", "appdb")
	press(t, m, "enter")
	if !contains(visible(m), "sibling_a") {
		t.Fatal("precondition: annex populated")
	}
	m.cursor = indexOf2(t, m, "pgc")
	press(t, m, "ctrl+c")
	if contains(visible(m), "sibling_a") {
		t.Fatalf("disconnect must clear the connection's annex entries: %v", visible(m))
	}
}
