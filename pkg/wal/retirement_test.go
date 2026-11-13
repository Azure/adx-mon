package wal

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Optional hooks let this regression run against the original implementation:
// without lifecycle cleanup the state assertions below fail, rather than compilation.
func releaseHandle(v any) {
	if h, ok := v.(interface{ Release() }); ok {
		h.Release()
	}
}
func retireState(v any, now time.Time, work int) {
	if h, ok := v.(interface{ retireIdle(time.Time, int) }); ok {
		h.retireIdle(now, work)
	}
}

func TestRetireDrainedDestinations(t *testing.T) {
	a := NewAdmission(AdmissionOpts{})
	r := NewRepository(RepositoryOpts{StorageDir: t.TempDir(), Admission: a})
	require.NoError(t, r.Open(t.Context()))
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	for i := range 40 {
		w, err := r.Get(t.Context(), fmt.Appendf(nil, "DB_T%d_schema", i))
		require.NoError(t, err)
		require.NoError(t, w.Write(t.Context(), []byte("payload")))
		w.closeActiveSegment()
		for _, si := range r.index.Get(nil, w.opts.Prefix) {
			require.NoError(t, w.Remove(si.Path))
			r.index.Remove(si)
		}
		releaseHandle(w)
	}
	require.Equal(t, Usage{}, a.Usage())
	now := time.Now().Add(time.Hour)
	retireState(r, now, 7)
	require.Equal(t, 33, r.Count(), "one sweep performs at most its work budget")
	for range 10 {
		retireState(r, now, 7)
		retireState(a, now, 7)
	}
	require.Zero(t, r.Count(), "drained idle WALs must be released")
	require.Empty(t, a.tables, "drained idle destination budgets must be released")
	w, err := r.Get(t.Context(), []byte("DB_T0_schema"))
	require.NoError(t, err)
	defer releaseHandle(w)
	require.NoError(t, w.Write(t.Context(), []byte("new payload")))
	require.EqualValues(t, 1, a.Usage().Segments)
}

func TestRetirementProtectsOwnersAndReservations(t *testing.T) {
	a := NewAdmission(AdmissionOpts{})
	r := NewRepository(RepositoryOpts{StorageDir: t.TempDir(), Admission: a})
	require.NoError(t, r.Open(t.Context()))
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	w, err := r.Get(t.Context(), []byte("DB_A_schema"))
	require.NoError(t, err)
	now := time.Now().Add(time.Hour)
	retireState(r, now, 100)
	require.Equal(t, 1, r.Count(), "a checked-out writer pins its WAL even before its first write")
	require.NoError(t, w.Write(t.Context(), []byte("payload")))
	releaseHandle(w)
	retireState(r, now, 100)
	retireState(a, now, 100)
	require.Equal(t, 1, r.Count(), "active segments pin WAL state")
	w, err = r.Get(t.Context(), []byte("DB_A_schema"))
	require.NoError(t, err)
	w.closeActiveSegment()
	releaseHandle(w)
	retireState(r, now, 100)
	require.Equal(t, 1, r.Count(), "indexed backlog pins WAL state")
	w, err = r.Get(t.Context(), []byte("DB_A_schema"))
	require.NoError(t, err)
	for _, si := range r.index.Get(nil, w.opts.Prefix) {
		require.NoError(t, w.Remove(si.Path))
		r.index.Remove(si)
	}
	releaseHandle(w)
	retireState(r, now, 100)
	require.Zero(t, r.Count())

	d := Destination{Database: "DB", Table: "Pending"}
	b := a.Table(d)
	reservation, err := b.Reserve("pending", 0, 0)
	require.NoError(t, err)
	releaseHandle(b)
	retireState(a, now, 100)
	require.Contains(t, a.tables, d, "even a zero-byte outstanding reservation pins its budget")
	reservation.Release()
	retireState(a, now, 100)
	require.NotContains(t, a.tables, d)
}

func TestRetirementRacesWritesAndRotation(t *testing.T) {
	a := NewAdmission(AdmissionOpts{})
	r := NewRepository(RepositoryOpts{StorageDir: t.TempDir(), Admission: a, SegmentMaxSize: 64})
	require.NoError(t, r.Open(t.Context()))
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	var wg sync.WaitGroup
	var owners sync.Mutex
	live := make(map[*WAL]int)
	budgets := make(map[*TableBudget]int)
	for range 4 {
		wg.Go(func() {
			for range 80 {
				w, err := r.Get(context.Background(), []byte("DB_A_schema"))
				require.NoError(t, err)
				owners.Lock()
				live[w]++
				require.Len(t, live, 1, "one live WAL owner per prefix")
				owners.Unlock()
				require.NoError(t, w.Write(context.Background(), []byte("payload")))
				w.rotateSegmentIfNecessary()
				b := a.Table(Destination{Database: "DB", Table: "A"})
				owners.Lock()
				budgets[b]++
				require.Len(t, budgets, 1, "one live budget owner per destination")
				owners.Unlock()
				w.closeActiveSegment()
				for _, si := range r.index.Get(nil, "DB_A_schema") {
					require.NoError(t, w.Remove(si.Path))
					r.index.Remove(si)
				}
				owners.Lock()
				budgets[b]--
				if budgets[b] == 0 {
					delete(budgets, b)
				}
				live[w]--
				if live[w] == 0 {
					delete(live, w)
				}
				owners.Unlock()
				releaseHandle(b)
				releaseHandle(w)
			}
		})
	}
	wg.Go(func() {
		for range 320 {
			retireState(r, time.Now().Add(time.Hour), 10)
			retireState(a, time.Now().Add(time.Hour), 10)
		}
	})
	wg.Wait()
	require.NoError(t, r.Close())
	for _, si := range r.index.Get(nil, "DB_A_schema") {
		require.NoError(t, (&WAL{opts: WALOpts{Prefix: si.Prefix, Admission: a}}).Remove(si.Path))
		r.index.Remove(si)
	}
	retireState(r, time.Now().Add(time.Hour), 100)
	retireState(a, time.Now().Add(time.Hour), 100)
	require.Zero(t, r.Count())
	require.Empty(t, a.tables)
	require.Equal(t, Usage{}, a.Usage())
}

func TestRetirementPreservesCumulativeRejections(t *testing.T) {
	a := NewAdmission(AdmissionOpts{Default: Usage{Bytes: 1}})
	d := Destination{Database: "DB", Table: "A"}
	r, err := a.Reserve("p", d, 2, 0)
	require.NoError(t, err)
	r.Release()
	a.RecordRejection(d, 3, 42)
	retireState(a, time.Now(), 10)
	require.Len(t, a.tables, 1, "recently used state is retained during the idle grace period")
	retireState(a, time.Now().Add(time.Hour), 10)
	require.Empty(t, a.tables)
	would, records, bytes := a.RejectionTotals()
	require.EqualValues(t, 1, would)
	require.EqualValues(t, 3, records)
	require.EqualValues(t, 42, bytes)
	a.RecordRejection(d, 1, 10)
	would, records, bytes = a.RejectionTotals()
	require.EqualValues(t, 1, would)
	require.EqualValues(t, 4, records)
	require.EqualValues(t, 52, bytes)
}
