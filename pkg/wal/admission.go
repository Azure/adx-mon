package wal

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	flakeutil "github.com/Azure/adx-mon/pkg/flake"
)

// TableLimit overrides the default local backlog budget for a normalized table.
type TableLimit struct {
	Database    string `toml:"database" json:"database"`
	Table       string `toml:"table" json:"table"`
	MaxBytes    int64  `toml:"max-bytes" json:"max-bytes"`
	MaxSegments int64  `toml:"max-segments" json:"max-segments"`
}

type AdmissionOpts struct {
	Global    Usage
	Default   Usage
	Overrides []TableLimit

	// Enforce rejects writes that exceed a table budget. When false the budget
	// is still measured and reported, which lets operators size limits from
	// real traffic before any data is shed.
	Enforce bool
}

type TableOverloadedError struct {
	Destination Destination
	Reason      string
	RetryAfter  time.Duration
}

func (e *TableOverloadedError) Error() string {
	return fmt.Sprintf("table %s overloaded: %s", e.Destination, e.Reason)
}

// SegmentTooLargeError reports a payload that cannot fit in a destination's
// budget even when that destination is completely empty. Unlike
// TableOverloadedError this is not relieved by draining, so the sender must
// split the batch or an operator must raise the limit.
type SegmentTooLargeError struct {
	Destination Destination
	Size        int64
	Limit       int64
}

func (e *SegmentTooLargeError) Error() string {
	return fmt.Sprintf("payload for table %s is %d bytes, which exceeds its %d byte budget", e.Destination, e.Size, e.Limit)
}

type fileUsage struct {
	bytes     int64
	createdAt time.Time
}

// oldestFile returns the creation time of the oldest resident file. The caller
// must hold t.mu.
func (t *TableBudget) oldestLocked() time.Time {
	if !t.oldest.IsZero() {
		return t.oldest
	}
	for _, f := range t.files {
		if t.oldest.IsZero() || f.createdAt.Before(t.oldest) {
			t.oldest = f.createdAt
		}
	}
	return t.oldest
}

// TableBudget owns the admission state for one destination. Writers resolve it
// once and reuse it, so the write path touches only this destination's lock and
// the process-wide counters. Budgets for different tables never contend.
type TableBudget struct {
	a     *Admission
	d     Destination
	limit Usage

	mu              sync.Mutex
	usage           Usage
	throttled       string
	throttledSince  time.Time
	wouldReject     int64
	rejectedRecords int64
	rejectedBytes   int64
	lastLogged      time.Time
	files           map[string]fileUsage
	// oldest caches the oldest resident file time. It is invalidated only when
	// the oldest file is removed, so scrapes do not scan every file.
	oldest time.Time
}

// Admission reserves disk capacity before writes. All values are physical WAL
// bytes, including buffered writes and headers; rotation does not change usage.
type Admission struct {
	opts AdmissionOpts

	mu     sync.RWMutex
	tables map[Destination]*TableBudget

	// Global totals are claimed atomically so the instance limit cannot be
	// exceeded by concurrent reservations on different tables.
	totalBytes    atomic.Int64
	totalSegments atomic.Int64
}

func NewAdmission(opts AdmissionOpts) *Admission {
	return &Admission{opts: opts, tables: make(map[Destination]*TableBudget)}
}

func (a *Admission) resolveLimit(d Destination) Usage {
	for _, l := range a.opts.Overrides {
		if l.Database == d.Database && l.Table == d.Table {
			result := a.opts.Default
			if l.MaxBytes > 0 {
				result.Bytes = l.MaxBytes
			}
			if l.MaxSegments > 0 {
				result.Segments = l.MaxSegments
			}
			return result
		}
	}
	return a.opts.Default
}

// Table returns the budget for a destination, creating it on first use. The
// returned handle is stable and should be cached by writers.
func (a *Admission) Table(d Destination) *TableBudget {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	t := a.tables[d]
	a.mu.RUnlock()
	if t != nil {
		return t
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if t = a.tables[d]; t != nil {
		return t
	}
	t = &TableBudget{a: a, d: d, limit: a.resolveLimit(d), files: make(map[string]fileUsage)}
	a.tables[d] = t
	return t
}

func (a *Admission) Limit(d Destination) Usage {
	if a == nil {
		return Usage{}
	}
	return a.Table(d).limit
}

type Reservation struct {
	t     *TableBudget
	path  string
	usage Usage
	once  sync.Once
}

var reservationPool = sync.Pool{New: func() any { return new(Reservation) }}

// addWithin adds delta to counter unless that would exceed limit.
func addWithin(counter *atomic.Int64, delta, limit int64) bool {
	for {
		current := counter.Load()
		if limit > 0 && current+delta > limit {
			return false
		}
		if counter.CompareAndSwap(current, current+delta) {
			return true
		}
	}
}

func (a *Admission) Reserve(path string, d Destination, bytes, segments int64) (*Reservation, error) {
	if a == nil {
		return nil, nil
	}
	return a.Table(d).Reserve(path, bytes, segments)
}

// Reserve claims capacity for a pending write. The caller must call Finish with
// the bytes actually written and then Release.
func (t *TableBudget) Reserve(path string, bytes, segments int64) (*Reservation, error) {
	if t == nil {
		return nil, nil
	}

	t.mu.Lock()
	if t.throttled != "" && belowResume(t.usage, t.limit) {
		t.throttled = ""
		t.throttledSince = time.Time{}
	}
	reason := t.throttled
	if t.limit.Bytes > 0 && t.usage.Bytes+bytes > t.limit.Bytes {
		reason = "bytes"
	}
	if t.limit.Segments > 0 && t.usage.Segments+segments > t.limit.Segments {
		reason = "segments"
	}
	if reason != "" {
		t.throttled = reason
		if t.throttledSince.IsZero() {
			t.throttledSince = time.Now()
		}
		if t.a.opts.Enforce {
			t.mu.Unlock()
			return nil, &TableOverloadedError{t.d, reason, 5 * time.Second}
		}
		// Observe-only: record that the budget was exceeded, then admit.
		t.wouldReject++
	}
	t.usage.Bytes += bytes
	t.usage.Segments += segments
	t.mu.Unlock()

	// Claimed atomically: the check and the commit must not be separable or
	// concurrent reservations on other tables could jointly pass the limit.
	if !addWithin(&t.a.totalBytes, bytes, t.a.opts.Global.Bytes) {
		t.releaseLocal(bytes, segments)
		return nil, ErrMaxDiskUsageExceeded
	}
	if segments != 0 && !addWithin(&t.a.totalSegments, segments, t.a.opts.Global.Segments) {
		t.a.totalBytes.Add(-bytes)
		t.releaseLocal(bytes, segments)
		return nil, ErrMaxSegmentsExceeded
	}

	r := reservationPool.Get().(*Reservation)
	r.t = t
	r.path = path
	r.usage = Usage{bytes, segments}
	return r, nil
}

// releaseLocal rolls back a table's own accounting after the global claim was
// refused. The global counters were not committed, so they are untouched.
func (t *TableBudget) releaseLocal(bytes, segments int64) {
	t.mu.Lock()
	t.usage.Bytes -= bytes
	t.usage.Segments -= segments
	t.mu.Unlock()
}

// Release rolls back an unfinished reservation and returns its allocation to the
// pool. The caller must not access the reservation after Release.
func (r *Reservation) Release() {
	if r == nil {
		return
	}
	r.Finish(0)
	*r = Reservation{}
	reservationPool.Put(r)
}

func belowResume(u, l Usage) bool {
	return (l.Bytes <= 0 || u.Bytes <= l.Bytes-(l.Bytes+4)/5) && (l.Segments <= 0 || u.Segments <= l.Segments-(l.Segments+4)/5)
}

// Finish commits the bytes actually written, or rolls back with zero. Segment
// reservations are committed only when a segment header was created.
func (r *Reservation) Finish(bytes int64) {
	if r == nil || r.t == nil {
		return
	}
	r.once.Do(func() {
		t := r.t
		delta := bytes - r.usage.Bytes

		t.mu.Lock()
		t.usage.Bytes += delta
		if bytes == 0 {
			t.usage.Segments -= r.usage.Segments
		} else {
			f := t.files[r.path]
			if f.createdAt.IsZero() {
				f.createdAt = time.Now()
				if t.oldest.IsZero() || f.createdAt.Before(t.oldest) {
					t.oldest = f.createdAt
				}
			}
			f.bytes += bytes
			t.files[r.path] = f
		}
		t.mu.Unlock()

		if delta != 0 {
			t.a.totalBytes.Add(delta)
		}
		if bytes == 0 && r.usage.Segments != 0 {
			t.a.totalSegments.Add(-r.usage.Segments)
		}
	})
}

// Restore registers an existing, repaired segment before intake starts.
func (a *Admission) Restore(path string, d Destination, bytes int64) {
	if a == nil {
		return
	}
	t := a.Table(d)

	createdAt := time.Now()
	if _, _, _, epoch, err := ParseFilename(path); err == nil {
		if ts, err := flakeutil.ParseFlakeID(epoch); err == nil {
			createdAt = ts
		}
	}

	t.mu.Lock()
	if _, ok := t.files[path]; ok {
		t.mu.Unlock()
		return
	}
	t.files[path] = fileUsage{bytes: bytes, createdAt: createdAt}
	if t.oldest.IsZero() || createdAt.Before(t.oldest) {
		t.oldest = createdAt
	}
	t.usage.Bytes += bytes
	t.usage.Segments++
	t.mu.Unlock()

	a.totalBytes.Add(bytes)
	a.totalSegments.Add(1)
}

// Forget must only be called after a file has been removed successfully.
func (a *Admission) Forget(d Destination, path string) {
	if a == nil {
		return
	}
	t := a.Table(d)

	t.mu.Lock()
	f, ok := t.files[path]
	if !ok {
		t.mu.Unlock()
		return
	}
	delete(t.files, path)
	if f.createdAt.Equal(t.oldest) {
		// The oldest file left; recompute lazily on the next read.
		t.oldest = time.Time{}
	}
	t.usage.Bytes -= f.bytes
	t.usage.Segments--
	t.mu.Unlock()

	a.totalBytes.Add(-f.bytes)
	a.totalSegments.Add(-1)
}

// Usage returns the exact totals across every table. It is a reporting path,
// not a write path, so it sums per-table state rather than reading the batched
// global estimate.
func (a *Admission) Usage() Usage {
	if a == nil {
		return Usage{}
	}
	a.mu.RLock()
	tables := make([]*TableBudget, 0, len(a.tables))
	for _, t := range a.tables {
		tables = append(tables, t)
	}
	a.mu.RUnlock()

	var total Usage
	for _, t := range tables {
		t.mu.Lock()
		total.Bytes += t.usage.Bytes
		total.Segments += t.usage.Segments
		t.mu.Unlock()
	}
	return total
}

func (a *Admission) Snapshot() map[Destination]Usage {
	result := make(map[Destination]Usage)
	for d, s := range a.Status() {
		result[d] = s.Usage
	}
	return result
}

type TableStatus struct {
	Usage
	Limit                          Usage
	Throttled                      bool
	Enforced                       bool
	WouldReject                    int64
	RejectedRecords, RejectedBytes int64
	OldestAge                      time.Duration

	// ThrottledFor is how long this destination has been continuously over
	// budget. A brief value is a burst; a sustained one means the destination
	// either needs a larger budget or is genuinely producing more than this
	// instance can drain.
	ThrottledFor time.Duration
}

func (a *Admission) Status() map[Destination]TableStatus {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	tables := make([]*TableBudget, 0, len(a.tables))
	for _, t := range a.tables {
		tables = append(tables, t)
	}
	a.mu.RUnlock()

	result := make(map[Destination]TableStatus, len(tables))
	for _, t := range tables {
		t.mu.Lock()
		if t.usage.Bytes == 0 && t.usage.Segments == 0 {
			// Retired: no resident backlog, so the gauge is dropped to bound
			// label cardinality. Cumulative shed counters live in the
			// component metrics and are unaffected.
			t.mu.Unlock()
			continue
		}
		status := TableStatus{
			Usage:           t.usage,
			Limit:           t.limit,
			Throttled:       t.throttled != "" && !belowResume(t.usage, t.limit),
			Enforced:        a.opts.Enforce,
			WouldReject:     t.wouldReject,
			RejectedRecords: t.rejectedRecords,
			RejectedBytes:   t.rejectedBytes,
		}
		if status.Throttled && !t.throttledSince.IsZero() {
			status.ThrottledFor = time.Since(t.throttledSince)
		}
		if oldest := t.oldestLocked(); !oldest.IsZero() {
			status.OldestAge = time.Since(oldest)
		}
		t.mu.Unlock()
		result[t.d] = status
	}
	return result
}

// Throttled reports whether a destination is currently over budget and
// enforcement is active. It is a cheap pre-admission check that lets callers
// skip encoding and WAL creation for work that will be rejected anyway.
func (a *Admission) Throttled(d Destination) (string, bool) {
	if a == nil || !a.opts.Enforce {
		return "", false
	}
	t := a.Table(d)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.throttled == "" || belowResume(t.usage, t.limit) {
		return "", false
	}
	return t.throttled, true
}

// RecordRejection accounts for records shed at a table budget and reports
// whether this rejection should be logged, rate limited to once per minute.
func (a *Admission) RecordRejection(d Destination, records, bytes int64) bool {
	if a == nil {
		return false
	}
	t := a.Table(d)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rejectedRecords += records
	t.rejectedBytes += bytes
	shouldLog := time.Since(t.lastLogged) >= time.Minute
	if shouldLog {
		t.lastLogged = time.Now()
	}
	return shouldLog
}
