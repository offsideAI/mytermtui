package icloud

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeBridge records calls and can fail on demand.
type fakeBridge struct {
	mu        sync.Mutex
	started   []string
	evicted   []string
	failStart map[string]error
}

func (f *fakeBridge) StartDownload(p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failStart[p]; err != nil {
		return err
	}
	f.started = append(f.started, p)
	return nil
}
func (f *fakeBridge) Evict(p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.evicted = append(f.evicted, p)
	return nil
}
func (f *fakeBridge) Trash(string) (string, error) { return "", errors.New("not used") }

// fakeFS simulates per-path materialization state.
type fakeFS struct {
	mu    sync.Mutex
	size  map[string]int64
	local map[string]int64
	gone  map[string]bool
}

func newFakeFS() *fakeFS {
	return &fakeFS{size: map[string]int64{}, local: map[string]int64{}, gone: map[string]bool{}}
}

func (f *fakeFS) set(path string, size, local int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.size[path] = size
	f.local[path] = local
}

func (f *fakeFS) stat(path string) (int64, int64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gone[path] {
		return 0, 0, false, errors.New("no such file")
	}
	size, ok := f.size[path]
	if !ok {
		return 0, 0, false, errors.New("no such file")
	}
	local := f.local[path]
	return size, local, local < size, nil
}

func newTestQueue(t *testing.T, b Bridge, fs *fakeFS, max int) *Queue {
	t.Helper()
	q := NewQueue(b, max, "")
	q.SetStatFunc(fs.stat)
	base := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	tick := 0
	q.SetNowFunc(func() time.Time {
		tick++
		return base.Add(time.Duration(tick) * time.Second)
	})
	return q
}

func TestQueueLifecycle(t *testing.T) {
	b := &fakeBridge{}
	fs := newFakeFS()
	fs.set("/a", 1000, 0)
	q := newTestQueue(t, b, fs, 2)

	if n := q.Add("/a"); n != 1 {
		t.Fatalf("Add = %d, want 1", n)
	}
	// duplicate add is ignored
	if n := q.Add("/a"); n != 0 {
		t.Fatalf("duplicate Add = %d, want 0", n)
	}

	s := q.Tick() // starts the download
	if len(b.started) != 1 || b.started[0] != "/a" {
		t.Fatalf("StartDownload calls = %v", b.started)
	}
	if s.Items[0].State != StateStarting && s.Items[0].State != StateDownloading {
		t.Fatalf("state after first tick = %v", s.Items[0].State)
	}

	fs.set("/a", 1000, 400)
	s = q.Tick()
	if s.Items[0].State != StateDownloading || s.Items[0].Got != 400 {
		t.Fatalf("mid-download: state=%v got=%d", s.Items[0].State, s.Items[0].Got)
	}

	fs.set("/a", 1000, 1000) // fully materialized → not dataless
	s = q.Tick()
	if s.Items[0].State != StateDone || s.Items[0].Got != 1000 {
		t.Fatalf("done: state=%v got=%d", s.Items[0].State, s.Items[0].Got)
	}
	if q.HasActive() {
		t.Fatal("queue should be idle")
	}
}

func TestQueueSkipsLocalAndMissing(t *testing.T) {
	b := &fakeBridge{}
	fs := newFakeFS()
	fs.set("/local", 500, 500) // already materialized
	q := newTestQueue(t, b, fs, 1)
	if n := q.Add("/local", "/missing"); n != 0 {
		t.Fatalf("Add = %d, want 0", n)
	}
}

func TestQueueConcurrencyCap(t *testing.T) {
	b := &fakeBridge{}
	fs := newFakeFS()
	fs.set("/a", 100, 0)
	fs.set("/b", 100, 0)
	fs.set("/c", 100, 0)
	q := newTestQueue(t, b, fs, 1)
	q.Add("/a", "/b", "/c")

	q.Tick()
	if len(b.started) != 1 {
		t.Fatalf("in-flight after tick 1 = %v, want just /a", b.started)
	}
	fs.set("/a", 100, 100)
	q.Tick() // /a completes; next tick may start /b
	q.Tick()
	if len(b.started) != 2 || b.started[1] != "/b" {
		t.Fatalf("started = %v, want [/a /b]", b.started)
	}
}

func TestQueueFailedStart(t *testing.T) {
	b := &fakeBridge{failStart: map[string]error{"/bad": errors.New("no account")}}
	fs := newFakeFS()
	fs.set("/bad", 100, 0)
	q := newTestQueue(t, b, fs, 1)
	q.Add("/bad")
	s := q.Tick()
	if s.Items[0].State != StateFailed || s.Items[0].Err == "" {
		t.Fatalf("state = %v err=%q, want failed", s.Items[0].State, s.Items[0].Err)
	}
}

func TestQueueCancelEvictsPartial(t *testing.T) {
	b := &fakeBridge{}
	fs := newFakeFS()
	fs.set("/a", 1000, 0)
	q := newTestQueue(t, b, fs, 1)
	q.Add("/a")
	q.Tick() // started
	q.Cancel(0)
	if len(b.evicted) != 1 || b.evicted[0] != "/a" {
		t.Fatalf("evicted = %v, want [/a]", b.evicted)
	}
	if q.HasActive() {
		t.Fatal("cancelled item should not be active")
	}
}

func TestQueueStallDetection(t *testing.T) {
	b := &fakeBridge{}
	fs := newFakeFS()
	fs.set("/a", 1000, 0)
	q := NewQueue(b, 1, "")
	q.SetStatFunc(fs.stat)
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	q.SetNowFunc(func() time.Time { return now })
	q.Add("/a")
	q.Tick()
	now = now.Add(31 * time.Second) // no byte progress for >30s
	s := q.Tick()
	if s.Items[0].State != StateStalled {
		t.Fatalf("state = %v, want stalled", s.Items[0].State)
	}
	// progress resumes → back to downloading
	fs.set("/a", 1000, 10)
	s = q.Tick()
	if s.Items[0].State != StateDownloading {
		t.Fatalf("state = %v, want downloading after resume", s.Items[0].State)
	}
}

func TestQueuePersistence(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "queue.json")
	b := &fakeBridge{}
	fs := newFakeFS()
	fs.set("/a", 100, 0)
	fs.set("/b", 100, 0)

	q1 := NewQueue(b, 1, stateFile)
	q1.SetStatFunc(fs.stat)
	q1.Add("/a", "/b")

	q2 := NewQueue(b, 1, stateFile)
	q2.SetStatFunc(fs.stat)
	q2.Restore()
	s := q2.Snapshot()
	if len(s.Items) != 2 {
		t.Fatalf("restored %d items, want 2", len(s.Items))
	}
	for _, it := range s.Items {
		if it.State != StateQueued {
			t.Fatalf("restored state = %v, want queued", it.State)
		}
	}
}

func TestQueuePauseAndMove(t *testing.T) {
	b := &fakeBridge{}
	fs := newFakeFS()
	fs.set("/a", 100, 0)
	fs.set("/b", 100, 0)
	q := newTestQueue(t, b, fs, 1)
	q.Add("/a", "/b")
	q.SetPaused(true)
	q.Tick()
	if len(b.started) != 0 {
		t.Fatalf("paused queue started %v", b.started)
	}
	q.Move(1, -1) // /b to front
	q.SetPaused(false)
	q.Tick()
	if len(b.started) != 1 || b.started[0] != "/b" {
		t.Fatalf("started = %v, want [/b] after reorder", b.started)
	}
}
