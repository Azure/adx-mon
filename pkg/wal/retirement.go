package wal

import "time"

const (
	destinationIdleTimeout = 5 * time.Minute
	retirementWork         = 128
)

// Release ends one lease acquired by Repository.Get. The caller must no longer
// use this handle unless it acquires it again. Standalone WALs are unaffected.
func (w *WAL) Release() {
	if w == nil || w.repository == nil {
		return
	}
	r := w.repository
	r.lifecycle.Lock()
	defer r.lifecycle.Unlock()
	if w.users <= 0 {
		panic("wal: Release without Get")
	}
	w.users--
	if w.users == 0 {
		w.idleSince = time.Now()
	}
}

// retireIdle visits a bounded round-robin batch. Busy rotations are skipped,
// rather than waiting on I/O while acquisitions are blocked. Lock order is
// repository.lifecycle -> WAL.rotateMu -> WAL.mu -> index/scheduler.
func (r *Repository) retireIdle(now time.Time, work int) {
	r.lifecycle.Lock()
	defer r.lifecycle.Unlock()
	for n := min(work, r.idleWALs.Len()); n > 0; n-- {
		e := r.idleWALs.Front()
		r.idleWALs.MoveToBack(e)
		w := e.Value.(*WAL)
		if w.users != 0 || now.Sub(w.idleSince) < destinationIdleTimeout || !w.rotateMu.TryLock() {
			continue
		}
		if !w.mu.TryLock() {
			w.rotateMu.Unlock()
			continue
		}
		r.index.mu.RLock()
		drained := w.segment == nil && w.inflightWriteBytes.Load() == 0 && len(r.index.segments[w.opts.Prefix]) == 0
		r.index.mu.RUnlock()
		if drained {
			w.closed = true
			w.setSegment(nil)
		}
		// Do not acquire a map partition while holding w.mu: Each readers
		// (debugging/sampling) hold the partition while reading the WAL.
		w.mu.Unlock()
		if drained {
			r.wals.Delete(w.opts.Prefix)
			r.idleWALs.Remove(e)
			w.idleEntry = nil
		}
		w.rotateMu.Unlock()
	}
}

// Release ends a budget lease acquired by Admission.Table. Reservations hold
// their own pin until Release, so they may outlive this lease.
func (t *TableBudget) Release() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.users <= 0 {
		panic("wal: budget Release without Table")
	}
	t.users--
	if t.users == 0 {
		t.idleSince = time.Now()
	}
}

// retireIdle never removes a leased budget, a reservation (even one for zero
// bytes), or resident file accounting. Table acquisition and removal share a.mu,
// so there can never be two live budgets for a destination.
func (a *Admission) retireIdle(now time.Time, work int) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for n := min(work, a.idleTables.Len()); n > 0; n-- {
		e := a.idleTables.Front()
		a.idleTables.MoveToBack(e)
		t := e.Value.(*TableBudget)
		if !t.mu.TryLock() {
			continue
		}
		if t.users == 0 && t.pending == 0 && t.usage == (Usage{}) && len(t.files) == 0 && now.Sub(t.idleSince) >= destinationIdleTimeout {
			delete(a.tables, t.d)
			a.idleTables.Remove(e)
		}
		t.mu.Unlock()
	}
}
