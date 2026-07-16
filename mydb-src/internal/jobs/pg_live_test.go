package jobs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Live pg_dump/pg_restore round-trip. Gated on MYDB_TEST_PG_DSN and the
// tools being on PATH.
func TestPGDumpRestoreLive(t *testing.T) {
	dsn := os.Getenv("MYDB_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("MYDB_TEST_PG_DSN not set")
	}
	if _, ok := Tool("pg_dump"); !ok {
		t.Skip("pg_dump not on PATH")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pc := PGConn{Host: cfg.Host, Port: int(cfg.Port), DBName: cfg.Database,
		Username: cfg.User, Password: cfg.Password}

	dest := filepath.Join(t.TempDir(), "dump.pgc")
	var phases []string
	err = PGDumpBackup(pc, dest)(context.Background(), func(_, _ int64, phase string) {
		if phase != "" {
			phases = append(phases, phase)
		}
	})
	if err != nil {
		t.Fatalf("pg_dump: %v", err)
	}
	fi, err := os.Stat(dest)
	if err != nil || fi.Size() == 0 {
		t.Fatalf("dump artifact missing or empty: %v", err)
	}
}

func TestPGDumpRefusesExisting(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "x")
	os.WriteFile(dest, []byte("x"), 0o644)
	pc := PGConn{Host: "localhost", DBName: "postgres"}
	err := PGDumpBackup(pc, dest)(context.Background(), func(int64, int64, string) {})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("should refuse existing dest: %v", err)
	}
}

func TestPGDumpCancel(t *testing.T) {
	if _, ok := Tool("pg_dump"); !ok {
		t.Skip("pg_dump not on PATH")
	}
	if os.Getenv("MYDB_TEST_PG_DSN") == "" {
		t.Skip("MYDB_TEST_PG_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	dest := filepath.Join(t.TempDir(), fmt.Sprintf("d%d.pgc", time.Now().UnixNano()))
	// A near-immediate cancel should not leave a completed artifact behind.
	pc := PGConn{Host: "localhost", DBName: "postgres"}
	_ = PGDumpBackup(pc, dest)(ctx, func(int64, int64, string) {})
	if _, err := os.Stat(dest); err == nil {
		t.Error("cancelled dump left an artifact")
	}
}
