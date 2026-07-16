package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pumpJobs ticks the model until the jobs queue goes idle (or times out),
// draining the async jobsTick messages the same way the runtime would.
func pumpJobs(t *testing.T, m *Model) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for m.jobsQ.HasActive() {
		apply(t, m, jobsTickMsg{})
		if time.Now().After(deadline) {
			t.Fatal("jobs did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	apply(t, m, jobsTickMsg{}) // final tick delivers the completion note
}

func TestSQLiteBackupThroughUI(t *testing.T) {
	m, _ := fixture(t)
	dest := filepath.Join(t.TempDir(), "out", "app-backup.db")
	m.cfg.Backup.Dir = filepath.Dir(dest)

	m.cursor = indexOf2(t, m, "app")
	press(t, m, "b") // backup → path dialog
	im, ok := m.modal.(*InputModal)
	if !ok {
		t.Fatalf("b should open the backup path dialog, modal=%#v", m.modal)
	}
	im.Input.SetValue(dest)
	press(t, m, "enter") // enqueue

	pumpJobs(t, m)

	snap := m.jobsQ.Snapshot()
	if len(snap.Jobs) != 1 || snap.Jobs[0].State.String() != "done" {
		t.Fatalf("backup job: %+v", snap.Jobs)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("backup file missing: %v", err)
	}
	// It was logged to jobs_history.
	hist, err := m.reg.Jobs(10)
	if err != nil || len(hist) != 1 || !hist[0].OK || hist[0].Kind != "backup" {
		t.Fatalf("jobs_history: %+v err=%v", hist, err)
	}
}

func TestJobsTabAndCancel(t *testing.T) {
	m, _ := fixture(t)
	press(t, m, "down", "enter") // connect
	press(t, m, "Q")             // jump to Jobs tab
	if m.tab != tabJobs || !m.focusRight {
		t.Fatalf("Q should focus the Jobs tab (tab=%d focus=%v)", m.tab, m.focusRight)
	}
	if !strings.Contains(m.View(), "no jobs") {
		t.Error("empty Jobs tab should say so")
	}
}

func TestJobBarHidesWhenIdle(t *testing.T) {
	m, _ := fixture(t)
	dest := filepath.Join(t.TempDir(), "b.db")
	m.cursor = indexOf2(t, m, "app")
	m.enqueueBackup(m.connByID[m.currentNode().ConnID], dest)
	pumpJobs(t, m)
	if m.jobBarVisible() {
		t.Error("job bar should hide once idle")
	}
}
