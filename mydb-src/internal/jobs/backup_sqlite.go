package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"
)

// SQLiteBackup returns a Run that backs up srcPath to dest via
// `VACUUM INTO` — an online, consistent, defragmented copy (spec §1.2).
// A dedicated read-only connection is used so a long backup does not
// block browsing on the app's live handle. Progress is the destination
// file's growing size against the source size (a real percentage).
func SQLiteBackup(srcPath, dest string) Run {
	return func(ctx context.Context, report Report) error {
		total := int64(-1)
		if fi, err := os.Stat(srcPath); err == nil {
			total = fi.Size()
		}
		if _, err := os.Stat(dest); err == nil {
			return fmt.Errorf("%s already exists", dest)
		}
		report(0, total, "vacuuming")

		db, err := sql.Open("sqlite", "file:"+srcPath+"?mode=ro&_pragma=busy_timeout(5000)")
		if err != nil {
			return err
		}
		defer db.Close()
		db.SetMaxOpenConns(1)

		// Poll the artifact size while VACUUM INTO runs.
		stop := make(chan struct{})
		go func() {
			t := time.NewTicker(200 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-t.C:
					if fi, err := os.Stat(dest); err == nil {
						report(fi.Size(), total, "vacuuming")
					}
				}
			}
		}()

		_, err = db.ExecContext(ctx, "VACUUM INTO "+quoteLiteral(dest))
		close(stop)
		if err != nil {
			os.Remove(dest) // no half-written artifact
			return err
		}
		if fi, err := os.Stat(dest); err == nil {
			report(fi.Size(), fi.Size(), "done")
		}
		return nil
	}
}

// SQLiteIntegrityBefore wraps a backup with a quick integrity check,
// refusing to copy a corrupt database.
func SQLiteIntegrityCheck(srcPath string) error {
	db, err := sql.Open("sqlite", "file:"+srcPath+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	var result string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&result); err != nil {
		return err
	}
	if strings.TrimSpace(result) != "ok" {
		return fmt.Errorf("integrity check failed: %s", result)
	}
	return nil
}

func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
