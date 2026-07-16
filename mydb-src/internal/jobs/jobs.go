// Package jobs is mydb's background work queue: backups, restores, and
// long maintenance run in goroutines while browsing continues. Adapted
// from the sibling apps' download queue, but jobs actually execute (the
// download queue delegated to fileproviderd), so each running job owns a
// goroutine and reports progress through a locked callback. The UI ticks
// on a timer to snapshot; nothing here persists across restarts (a
// killed pg_dump can only be restarted) — finished jobs are logged by
// the caller.
package jobs

import (
	"context"
	"sync"
	"time"
)

type Kind int

const (
	KindBackup Kind = iota
	KindRestore
	KindMaintenance
	KindExport
)

func (k Kind) String() string {
	switch k {
	case KindBackup:
		return "backup"
	case KindRestore:
		return "restore"
	case KindMaintenance:
		return "maintenance"
	case KindExport:
		return "export"
	}
	return "job"
}

type State int

const (
	Queued State = iota
	Running
	Done
	Failed
	Cancelled
)

func (s State) String() string {
	switch s {
	case Queued:
		return "queued"
	case Running:
		return "running"
	case Done:
		return "done"
	case Failed:
		return "failed"
	case Cancelled:
		return "cancelled"
	}
	return "?"
}

func (s State) Active() bool { return s == Queued || s == Running }

// Report is how a runner publishes progress. bytesDone is the artifact
// size so far; phase is a human label ("dumping public.users"); pass ""
// to leave the phase unchanged. total is the expected size, -1 unknown.
type Report func(bytesDone, total int64, phase string)

// Run does the work. It must honor ctx cancellation and use report for
// progress. The returned error (nil = success) sets the final state.
type Run func(ctx context.Context, report Report) error

// Job is one unit of background work. Fields other than the immutable
// header are guarded by the queue mutex.
type Job struct {
	ID     int
	Kind   Kind
	ConnID int64
	Label  string
	Dest   string

	State     State
	BytesDone int64
	BytesTot  int64 // -1 unknown
	Phase     string
	Err       string
	Started   time.Time
	Finished  time.Time

	run    Run
	cancel context.CancelFunc
}

// Snapshot is an immutable view for rendering.
type Snapshot struct {
	Jobs      []Job
	RunningN  int
	QueuedN   int
	ActiveN   int
	FirstDest string // Dest of the first running job, for the job bar
}

// Completion reports a finished job so the caller can log it; delivered once
// per job as it completes.
type Completion struct {
	Job Job
}

// Queue runs jobs with a global concurrency cap and at most one job per
// connection (a server shouldn't fight itself over a dump).
type Queue struct {
	mu      sync.Mutex
	jobs    []*Job
	max     int
	seq     int
	now     func() time.Time
	onDone  func(Completion)
	pending []Completion // completions awaiting delivery on the next Tick
}

// New creates a queue capping concurrent jobs at max (min 1). onDone is
// invoked (from Tick, on the UI goroutine) once per completed job.
func New(max int, onDone func(Completion)) *Queue {
	if max < 1 {
		max = 1
	}
	return &Queue{max: max, now: time.Now, onDone: onDone}
}

// SetNowFunc overrides the clock for tests.
func (q *Queue) SetNowFunc(f func() time.Time) { q.now = f }

// Add enqueues a job and returns its assigned ID.
func (q *Queue) Add(kind Kind, connID int64, label, dest string, run Run) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.seq++
	q.jobs = append(q.jobs, &Job{
		ID: q.seq, Kind: kind, ConnID: connID, Label: label, Dest: dest,
		State: Queued, BytesTot: -1, run: run,
	})
	return q.seq
}

// Tick starts eligible queued jobs and returns a fresh snapshot. It also
// flushes completion callbacks for jobs that finished since the last
// tick (so onDone runs on the UI goroutine, not a worker).
func (q *Queue) Tick() Snapshot {
	q.mu.Lock()
	pending := q.pending
	q.pending = nil

	running := 0
	busyConn := map[int64]bool{}
	for _, j := range q.jobs {
		if j.State == Running {
			running++
			busyConn[j.ConnID] = true
		}
	}
	for _, j := range q.jobs {
		if running >= q.max {
			break
		}
		if j.State != Queued || busyConn[j.ConnID] {
			continue
		}
		q.startLocked(j)
		busyConn[j.ConnID] = true
		running++
	}
	snap := q.snapshotLocked()
	q.mu.Unlock()

	for _, d := range pending {
		if q.onDone != nil {
			q.onDone(d)
		}
	}
	return snap
}

// startLocked launches a job's goroutine (caller holds mu).
func (q *Queue) startLocked(j *Job) {
	ctx, cancel := context.WithCancel(context.Background())
	j.cancel = cancel
	j.State = Running
	j.Started = q.now()
	// A negative bytesDone/total means "leave that field unchanged" — so a
	// phase-only update (report(-1, -1, "dumping …")) doesn't clobber the
	// byte counter, and vice versa.
	report := func(bytesDone, total int64, phase string) {
		q.mu.Lock()
		if j.State == Running {
			if bytesDone >= 0 {
				j.BytesDone = bytesDone
			}
			if total >= 0 {
				j.BytesTot = total
			}
			if phase != "" {
				j.Phase = phase
			}
		}
		q.mu.Unlock()
	}
	go func() {
		err := j.run(ctx, report)
		q.mu.Lock()
		j.Finished = q.now()
		switch {
		case ctx.Err() != nil:
			j.State = Cancelled
		case err != nil:
			j.State = Failed
			j.Err = err.Error()
		default:
			j.State = Done
		}
		q.pending = append(q.pending, Completion{Job: j.header()})
		q.mu.Unlock()
	}()
}

// Cancel stops a job: a running one via its context, a queued one
// straight to Cancelled.
func (q *Queue) Cancel(id int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, j := range q.jobs {
		if j.ID != id {
			continue
		}
		switch j.State {
		case Running:
			if j.cancel != nil {
				j.cancel()
			}
		case Queued:
			j.State = Cancelled
			j.Finished = q.now()
			q.pending = append(q.pending, Completion{Job: j.header()})
		}
		return
	}
}

// ClearFinished drops non-active jobs from the list.
func (q *Queue) ClearFinished() {
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := q.jobs[:0]
	for _, j := range q.jobs {
		if j.State.Active() {
			kept = append(kept, j)
		}
	}
	q.jobs = kept
}

// HasActive reports whether anything still needs ticking.
func (q *Queue) HasActive() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, j := range q.jobs {
		if j.State.Active() {
			return true
		}
	}
	return len(q.pending) > 0
}

// Snapshot returns the current view without advancing anything.
func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.snapshotLocked()
}

func (q *Queue) snapshotLocked() Snapshot {
	s := Snapshot{}
	for _, j := range q.jobs {
		s.Jobs = append(s.Jobs, j.header())
		switch j.State {
		case Running:
			s.RunningN++
			s.ActiveN++
			if s.FirstDest == "" {
				s.FirstDest = j.Dest
			}
		case Queued:
			s.QueuedN++
			s.ActiveN++
		}
	}
	return s
}

// header copies the render-visible fields (never the func pointers).
func (j *Job) header() Job {
	return Job{
		ID: j.ID, Kind: j.Kind, ConnID: j.ConnID, Label: j.Label, Dest: j.Dest,
		State: j.State, BytesDone: j.BytesDone, BytesTot: j.BytesTot,
		Phase: j.Phase, Err: j.Err, Started: j.Started, Finished: j.Finished,
	}
}
