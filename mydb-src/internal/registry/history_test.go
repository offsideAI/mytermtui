package registry

import (
	"fmt"
	"testing"
)

func TestHistoryAppendScopePrune(t *testing.T) {
	r := openTemp(t)
	id1, err := r.Create(Connection{Name: "a", Engine: "sqlite", Locality: "local", Path: "/a"})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := r.Create(Connection{Name: "b", Engine: "sqlite", Locality: "local", Path: "/b"})
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		conn := id1
		if i%2 == 1 {
			conn = id2
		}
		if err := r.AddHistory(HistoryEntry{
			ConnectionID: conn, SQL: fmt.Sprintf("SELECT %d", i),
			StartedAt: "2026-07-15T00:00:00Z", OK: true, Rows: int64(i),
		}, 0); err != nil {
			t.Fatal(err)
		}
	}

	all, err := r.History(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 || all[0].SQL != "SELECT 4" || all[0].ConnName == "" {
		t.Fatalf("all history wrong: %+v", all)
	}
	scoped, err := r.History(id1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped) != 3 {
		t.Fatalf("scoped history: want 3, got %d", len(scoped))
	}

	// Prune to 2 on the next insert.
	if err := r.AddHistory(HistoryEntry{ConnectionID: id1, SQL: "SELECT 99",
		StartedAt: "2026-07-15T00:00:01Z", OK: false, Error: "boom"}, 2); err != nil {
		t.Fatal(err)
	}
	all, _ = r.History(0, 100)
	if len(all) != 2 || all[0].SQL != "SELECT 99" || all[0].OK || all[0].Error != "boom" {
		t.Fatalf("prune wrong: %+v", all)
	}

	// Deleting a connection cascades its history away.
	if err := r.Delete(id1); err != nil {
		t.Fatal(err)
	}
	all, _ = r.History(0, 100)
	for _, e := range all {
		if e.ConnectionID == id1 {
			t.Fatalf("history for deleted connection survived: %+v", e)
		}
	}
}
