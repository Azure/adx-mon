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
	defer kept.Unlock()

	for i := range 5 {
		s.mappingLock(fmt.Sprintf("free-%d", i))
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
		s.mappingLock(fmt.Sprintf("table:entity-%04d", i))
	}

	s.reclaimInflight()

	// None of the locks is held, so reclaim must drop them all.
	require.Empty(t, s.inflight, "idle per-entity locks must be reclaimed to bound growth")

	// Repeated cycles of new names must not accumulate locks.
	for round := range 3 {
		for i := range n {
			s.mappingLock(fmt.Sprintf("round-%d-entity-%04d", round, i))
		}
		s.reclaimInflight()
		require.Empty(t, s.inflight, "idle per-entity locks must not accumulate across reclaim cycles")
	}
}
