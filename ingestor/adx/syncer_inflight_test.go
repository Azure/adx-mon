package adx

import (
	"fmt"
	"testing"

	"github.com/Azure/adx-mon/schema"
	"github.com/stretchr/testify/require"
)

// A held lock must never be reclaimed: the goroutine doing schema work for that
// entity is still using it, and a caller racing it must block on the same mutex.
func TestInflightReclaimKeepsHeldLocks(t *testing.T) {
	s := NewSyncer(&fakeKustoMgmt{}, "db", schema.SchemaMapping{}, PromMetrics)

	kept := s.mappingLock("held")
	kept.Lock()
	defer s.releaseMappingLock("held", kept)

	for i := range 5 {
		name := fmt.Sprintf("free-%d", i)
		entry := s.mappingLock(name)
		entry.Lock()
		s.releaseMappingLock(name, entry)
	}

	_, ok := s.inflight["held"]
	require.True(t, ok, "the lock must exist while held")

	s.reclaimInflight()

	_, ok = s.inflight["held"]
	require.True(t, ok, "held lock must survive reclaim")
	for i := range 5 {
		require.NotContains(t, s.inflight, fmt.Sprintf("free-%d", i))
	}
}

// Every distinct entity name allocates a per-name mutex that is never freed, so
// the map grows without bound as new tables and mappings flow through.
func TestInflightReclaimBoundsGrowth(t *testing.T) {
	s := NewSyncer(&fakeKustoMgmt{}, "db", schema.SchemaMapping{}, PromMetrics)

	const n = 1000
	for i := range n {
		name := fmt.Sprintf("table:entity-%04d", i)
		entry := s.mappingLock(name)
		entry.Lock()
		s.releaseMappingLock(name, entry)
	}

	s.reclaimInflight()

	// None of the locks is held, so reclaim must drop them all.
	require.Empty(t, s.inflight, "idle per-entity locks must be reclaimed to bound growth")

	// Repeated cycles of new names must not accumulate locks.
	for round := range 3 {
		for i := range n {
			name := fmt.Sprintf("round-%d-entity-%04d", round, i)
			entry := s.mappingLock(name)
			entry.Lock()
			s.releaseMappingLock(name, entry)
		}
		s.reclaimInflight()
		require.Empty(t, s.inflight, "idle per-entity locks must not accumulate across reclaim cycles")
	}
}

func TestInflightReclaimKeepsWaiters(t *testing.T) {
	s := NewSyncer(&fakeKustoMgmt{}, "db", schema.SchemaMapping{}, PromMetrics)
	first := s.mappingLock("same")
	first.Lock()
	second := s.mappingLock("same") // Registered but not yet locked.
	s.reclaimInflight()
	require.Same(t, first, s.mappingLock("same"), "a waiter must not be separated from later callers")
	// Balance the third reference and release both original users.
	s.releaseMappingLock("same", first)
	second.Lock()
	s.releaseMappingLock("same", second)
	third := s.inflight["same"]
	third.Lock()
	s.releaseMappingLock("same", third)
	s.reclaimInflight()
	require.Empty(t, s.inflight)
}
