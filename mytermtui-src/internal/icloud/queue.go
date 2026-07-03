package icloud

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ItemState is the lifecycle of a queued download.
type ItemState int

const (
	StateQueued ItemState = iota
	StateStarting
	StateDownloading
	StateStalled
	StateDone
	StateFailed
	StateCancelled
)

func (s ItemState) String() string {
	switch s {
	case StateQueued:
		return "queued"
	case StateStarting:
		return "starting"
	case StateDownloading:
		return "downloading"
	case StateStalled:
		return "stalled"
	case StateDone:
		return "done"
	case StateFailed:
		return "failed"
	case StateCancelled:
		return "cancelled"
	}
	return "?"
}

// Item is one file in the download queue.
type Item struct {
	Path  string
	Size  int64
	Got   int64
	State ItemState
	Err   string

	lastGot    int64
	lastChange time.Time
}

// Active reports whether the item still needs queue attention.
func (it *Item) Active() bool {
	switch it.State {
	case StateQueued, StateStarting, StateDownloading, StateStalled:
		return true
	}
	return false
}

// Snapshot is an immutable view of the queue for rendering.
type Snapshot struct {
	Items      []Item
	Paused     bool
	ActiveN    int
	DoneN      int
	TotalN     int
	GotBytes   int64
	TotalBytes int64
	Current    string // path of first in-flight item
	CurrentPct float64
}

// StatFunc reports (logical size, locally-present bytes, dataless) for a
// path. Injectable for tests; production uses lstat.
type StatFunc func(path string) (size, local int64, dataless bool, err error)

func lstatFunc(path string) (int64, int64, bool, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return 0, 0, false, err
	}
	return fi.Size(), LocalBytes(fi), Dataless(fi), nil
}

const stallAfter = 30 * time.Second

// Queue drives iCloud downloads. It has no internal goroutine: the UI
// calls Tick on its poll interval, which starts pending items (up to the
// concurrency cap) and re-stats in-flight ones for progress.
type Queue struct {
	mu        sync.Mutex
	items     []*Item
	bridge    Bridge
	stat      StatFunc
	max       int
	paused    bool
	stateFile string
	now       func() time.Time
}

// NewQueue creates a queue backed by bridge, capping concurrent
// materializations at max. stateFile persists pending paths ("" disables).
func NewQueue(bridge Bridge, max int, stateFile string) *Queue {
	if max < 1 {
		max = 1
	}
	return &Queue{
		bridge:    bridge,
		stat:      lstatFunc,
		max:       max,
		stateFile: stateFile,
		now:       time.Now,
	}
}

// SetStatFunc overrides stat for tests.
func (q *Queue) SetStatFunc(f StatFunc) { q.stat = f }

// SetNowFunc overrides the clock for tests.
func (q *Queue) SetNowFunc(f func() time.Time) { q.now = f }

// Add enqueues paths (deduplicated against active items). Returns the
// number actually added.
func (q *Queue) Add(paths ...string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	active := map[string]bool{}
	for _, it := range q.items {
		if it.Active() {
			active[it.Path] = true
		}
	}
	added := 0
	for _, p := range paths {
		if active[p] {
			continue
		}
		size, local, dataless, err := q.stat(p)
		if err != nil || !dataless {
			continue // gone or already local: nothing to do
		}
		q.items = append(q.items, &Item{
			Path: p, Size: size, Got: local,
			State: StateQueued, lastChange: q.now(),
		})
		active[p] = true
		added++
	}
	q.persistLocked()
	return added
}

// Tick advances the queue: starts pending downloads and polls in-flight
// ones. Returns a fresh snapshot.
func (q *Queue) Tick() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()

	inflight := 0
	for _, it := range q.items {
		switch it.State {
		case StateStarting, StateDownloading, StateStalled:
			inflight++
		}
	}

	changed := false
	for _, it := range q.items {
		if q.paused || inflight >= q.max {
			break
		}
		if it.State != StateQueued {
			continue
		}
		if err := q.bridge.StartDownload(it.Path); err != nil {
			it.State = StateFailed
			it.Err = err.Error()
		} else {
			it.State = StateStarting
			it.lastChange = q.now()
			inflight++
		}
		changed = true
	}

	for _, it := range q.items {
		switch it.State {
		case StateStarting, StateDownloading, StateStalled:
		default:
			continue
		}
		size, local, dataless, err := q.stat(it.Path)
		if err != nil {
			it.State = StateFailed
			it.Err = err.Error()
			changed = true
			continue
		}
		it.Size = size
		if local > size {
			local = size
		}
		it.Got = local
		if !dataless {
			it.Got = size
			it.State = StateDone
			changed = true
			continue
		}
		if local > it.lastGot {
			it.lastGot = local
			it.lastChange = q.now()
			if it.State != StateDownloading {
				it.State = StateDownloading
				changed = true
			}
		} else if q.now().Sub(it.lastChange) > stallAfter && it.State != StateStalled {
			it.State = StateStalled
			changed = true
		}
	}

	if changed {
		q.persistLocked()
	}
	return q.snapshotLocked()
}

// Snapshot returns the current view without advancing anything.
func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.snapshotLocked()
}

func (q *Queue) snapshotLocked() Snapshot {
	s := Snapshot{Paused: q.paused, TotalN: len(q.items)}
	for _, it := range q.items {
		s.Items = append(s.Items, *it)
		switch {
		case it.Active():
			s.ActiveN++
			s.GotBytes += it.Got
			s.TotalBytes += it.Size
		case it.State == StateDone:
			s.DoneN++
			s.GotBytes += it.Size
			s.TotalBytes += it.Size
		}
		if s.Current == "" && (it.State == StateDownloading || it.State == StateStarting || it.State == StateStalled) {
			s.Current = it.Path
			if it.Size > 0 {
				s.CurrentPct = float64(it.Got) / float64(it.Size)
			}
		}
	}
	return s
}

// Cancel aborts the item at index (best-effort: evicts a partial download).
func (q *Queue) Cancel(idx int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if idx < 0 || idx >= len(q.items) {
		return
	}
	it := q.items[idx]
	if !it.Active() {
		return
	}
	started := it.State != StateQueued
	it.State = StateCancelled
	if started {
		_ = q.bridge.Evict(it.Path) // roll back partial materialization
	}
	q.persistLocked()
}

// CancelAll aborts every active item.
func (q *Queue) CancelAll() {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, it := range q.items {
		if !it.Active() {
			continue
		}
		started := it.State != StateQueued
		it.State = StateCancelled
		if started {
			_ = q.bridge.Evict(it.Path)
		}
	}
	q.persistLocked()
}

// SetPaused stops starting new downloads (in-flight ones continue:
// fileproviderd owns the transfer and cannot be paused per-file).
func (q *Queue) SetPaused(p bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.paused = p
}

// Move reorders a queued item by delta within the queue.
func (q *Queue) Move(idx, delta int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	to := idx + delta
	if idx < 0 || idx >= len(q.items) || to < 0 || to >= len(q.items) {
		return
	}
	q.items[idx], q.items[to] = q.items[to], q.items[idx]
	q.persistLocked()
}

// ClearFinished drops done/failed/cancelled items from the list.
func (q *Queue) ClearFinished() {
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := q.items[:0]
	for _, it := range q.items {
		if it.Active() {
			kept = append(kept, it)
		}
	}
	q.items = kept
	q.persistLocked()
}

// HasActive reports whether anything still needs ticking.
func (q *Queue) HasActive() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, it := range q.items {
		if it.Active() {
			return true
		}
	}
	return false
}

// --- persistence -----------------------------------------------------------

type queueState struct {
	Pending []string `json:"pending"`
}

func (q *Queue) persistLocked() {
	if q.stateFile == "" {
		return
	}
	var st queueState
	for _, it := range q.items {
		if it.Active() {
			st.Pending = append(st.Pending, it.Path)
		}
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(q.stateFile), 0o755)
	_ = os.WriteFile(q.stateFile, data, 0o644)
}

// Restore re-queues paths persisted by a previous session. Call after
// any SetStatFunc override (the paths are re-stat'ed on Add).
func (q *Queue) Restore() {
	if q.stateFile == "" {
		return
	}
	data, err := os.ReadFile(q.stateFile)
	if err != nil {
		return
	}
	var st queueState
	if json.Unmarshal(data, &st) != nil {
		return
	}
	if len(st.Pending) > 0 {
		q.Add(st.Pending...)
	}
}
