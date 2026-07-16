package registry

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func openTemp(t *testing.T) *Registry {
	t.Helper()
	r, err := Open(filepath.Join(t.TempDir(), "state", "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func TestOpenCreatesPrivateFile(t *testing.T) {
	r := openTemp(t)
	fi, err := os.Stat(r.Path())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("registry mode = %o, want 600", fi.Mode().Perm())
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
	for i := 0; i < 2; i++ {
		r, err := Open(path)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		r.Close()
	}
}

func TestConnectionCRUD(t *testing.T) {
	r := openTemp(t)

	id, err := r.Create(Connection{
		Name: "app", Engine: "sqlite", Locality: "local", Path: "/tmp/app.db",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Create(Connection{
		Name: "prod-pg", Engine: "postgres", Locality: "remote",
		Host: "db.example.com", Port: 5432, DBName: "appdb",
		Username: "admin", Secret: "s3cret",
	})
	if err != nil {
		t.Fatal(err)
	}

	conns, err := r.Connections()
	if err != nil {
		t.Fatal(err)
	}
	if len(conns) != 2 {
		t.Fatalf("want 2 connections, got %d", len(conns))
	}
	// Locals order before remotes.
	if conns[0].Name != "app" || conns[1].Name != "prod-pg" {
		t.Errorf("order: got %s, %s", conns[0].Name, conns[1].Name)
	}
	if conns[1].Secret != "s3cret" {
		t.Errorf("secret round-trip failed: %q", conns[1].Secret)
	}

	upd := conns[0]
	upd.Name = "app-v2"
	upd.Locality = "remote"
	if err := r.Update(upd); err != nil {
		t.Fatal(err)
	}
	conns, _ = r.Connections()
	if conns[0].Name != "app-v2" || conns[0].Locality != "remote" {
		t.Errorf("update not applied: %+v", conns[0])
	}

	if err := r.TouchUsed(id); err != nil {
		t.Fatal(err)
	}
	conns, _ = r.Connections()
	if conns[0].LastUsedAt == "" {
		t.Error("TouchUsed did not stamp last_used_at")
	}

	if err := r.Delete(id); err != nil {
		t.Fatal(err)
	}
	conns, _ = r.Connections()
	if len(conns) != 1 {
		t.Fatalf("want 1 connection after delete, got %d", len(conns))
	}
}

func TestDuplicateNameRejected(t *testing.T) {
	r := openTemp(t)
	if _, err := r.Create(Connection{Name: "x", Engine: "sqlite", Locality: "local"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Create(Connection{Name: "x", Engine: "sqlite", Locality: "local"}); err == nil {
		t.Fatal("duplicate connection name must be rejected")
	}
}
