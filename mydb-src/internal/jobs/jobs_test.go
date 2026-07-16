package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// waitUntil polls the queue via Tick until cond holds or the deadline
// passes; returns the last snapshot.
func waitUntil(t *testing.T, q *Queue, cond func(Snapshot) bool) Snapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		s := q.Tick()
		if cond(s) {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition not met; snapshot=%+v", s)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRunToCompletion(t *testing.T) {
	var doneMu sync.Mutex
	var doneJobs []Job
	q := New(2, func(d Completion) {
		doneMu.Lock()
		doneJobs = append(doneJobs, d.Job)
		doneMu.Unlock()
	})
	q.Add(KindBackup, 1, "backup app", "/tmp/app.dump", func(ctx context.Context, r Report) error {
		r(50, 100, "halfway")
		r(100, 100, "flushing")
		return nil
	})
	waitUntil(t, q, func(s Snapshot) bool { return s.ActiveN == 0 })

	doneMu.Lock()
	defer doneMu.Unlock()
	if len(doneJobs) != 1 || doneJobs[0].State != Done {
		t.Fatalf("done callback: %+v", doneJobs)
	}
	if doneJobs[0].BytesDone != 100 {
		t.Errorf("final bytes = %d", doneJobs[0].BytesDone)
	}
}

func TestFailurePropagates(t *testing.T) {
	q := New(2, nil)
	q.Add(KindBackup, 1, "x", "", func(ctx context.Context, r Report) error {
		return errors.New("disk full")
	})
	s := waitUntil(t, q, func(s Snapshot) bool { return s.ActiveN == 0 })
	if s.Jobs[0].State != Failed || s.Jobs[0].Err != "disk full" {
		t.Fatalf("job: %+v", s.Jobs[0])
	}
}

func TestGlobalConcurrencyCap(t *testing.T) {
	q := New(2, nil)
	var peak, cur int
	var mu sync.Mutex
	block := make(chan struct{})
	body := func(ctx context.Context, r Report) error {
		mu.Lock()
		cur++
		if cur > peak {
			peak = cur
		}
		mu.Unlock()
		<-block
		mu.Lock()
		cur--
		mu.Unlock()
		return nil
	}
	for i := 0; i < 4; i++ {
		q.Add(KindExport, int64(i), "j", "", body) // distinct connections
	}
	waitUntil(t, q, func(s Snapshot) bool { return s.RunningN == 2 })
	// Let it settle to confirm it never exceeds the cap.
	for i := 0; i < 5; i++ {
		q.Tick()
		time.Sleep(5 * time.Millisecond)
	}
	close(block)
	waitUntil(t, q, func(s Snapshot) bool { return s.ActiveN == 0 })
	mu.Lock()
	defer mu.Unlock()
	if peak != 2 {
		t.Fatalf("peak concurrency = %d, want 2", peak)
	}
}

func TestOnePerConnection(t *testing.T) {
	q := New(4, nil)
	var mu sync.Mutex
	running := 0
	peakSame := 0
	block := make(chan struct{})
	body := func(ctx context.Context, r Report) error {
		mu.Lock()
		running++
		if running > peakSame {
			peakSame = running
		}
		mu.Unlock()
		<-block
		mu.Lock()
		running--
		mu.Unlock()
		return nil
	}
	// Three jobs, all on connection 7.
	q.Add(KindBackup, 7, "a", "", body)
	q.Add(KindBackup, 7, "b", "", body)
	q.Add(KindBackup, 7, "c", "", body)
	for i := 0; i < 6; i++ {
		q.Tick()
		time.Sleep(5 * time.Millisecond)
	}
	close(block)
	waitUntil(t, q, func(s Snapshot) bool { return s.ActiveN == 0 })
	mu.Lock()
	defer mu.Unlock()
	if peakSame != 1 {
		t.Fatalf("same-connection concurrency = %d, want 1", peakSame)
	}
}

func TestCancelRunning(t *testing.T) {
	q := New(2, nil)
	started := make(chan struct{})
	id := q.Add(KindBackup, 1, "x", "", func(ctx context.Context, r Report) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	q.Tick()
	<-started
	q.Cancel(id)
	s := waitUntil(t, q, func(s Snapshot) bool { return s.ActiveN == 0 })
	if s.Jobs[0].State != Cancelled {
		t.Fatalf("state = %v", s.Jobs[0].State)
	}
}

func TestCancelQueued(t *testing.T) {
	q := New(1, nil)
	block := make(chan struct{})
	q.Add(KindBackup, 1, "busy", "", func(ctx context.Context, r Report) error { <-block; return nil })
	id := q.Add(KindBackup, 2, "waiting", "", func(ctx context.Context, r Report) error { return nil })
	waitUntil(t, q, func(s Snapshot) bool { return s.RunningN == 1 && s.QueuedN == 1 })
	q.Cancel(id)
	s := q.Snapshot()
	var waiting *Job
	for i := range s.Jobs {
		if s.Jobs[i].ID == id {
			waiting = &s.Jobs[i]
		}
	}
	if waiting == nil || waiting.State != Cancelled {
		t.Fatalf("queued cancel: %+v", waiting)
	}
	close(block)
	waitUntil(t, q, func(s Snapshot) bool { return s.ActiveN == 0 })
}

func TestClearFinished(t *testing.T) {
	q := New(2, nil)
	q.Add(KindBackup, 1, "x", "", func(ctx context.Context, r Report) error { return nil })
	waitUntil(t, q, func(s Snapshot) bool { return s.ActiveN == 0 })
	q.ClearFinished()
	if len(q.Snapshot().Jobs) != 0 {
		t.Fatal("finished jobs should be cleared")
	}
}
