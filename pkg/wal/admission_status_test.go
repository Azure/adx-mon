package wal

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOldestAgeTracksFileLifecycle(t *testing.T) {
	a := NewAdmission(AdmissionOpts{})
	d := Destination{Database: "DB", Table: "A"}

	now := time.Now()
	a.Restore("DB_A_s_0000000000000003.wal", d, 10)
	budget := a.Table(d)

	// Restore derives creation time from the flake id; override for determinism.
	budget.mu.Lock()
	budget.files["DB_A_s_0000000000000003.wal"] = fileUsage{bytes: 10, createdAt: now.Add(-time.Hour)}
	budget.files["newer"] = fileUsage{bytes: 10, createdAt: now.Add(-time.Minute)}
	budget.usage.Bytes += 10
	budget.usage.Segments++
	budget.oldest = time.Time{}
	budget.mu.Unlock()

	require.InDelta(t, time.Hour.Seconds(), a.Status()[d].OldestAge.Seconds(), 5)

	// Removing the oldest file must advance the reported age, not keep a stale
	// cached value.
	a.Forget(d, "DB_A_s_0000000000000003.wal")
	require.InDelta(t, time.Minute.Seconds(), a.Status()[d].OldestAge.Seconds(), 5)

	// Removing a non-oldest file must leave the age intact.
	budget.mu.Lock()
	budget.files["newest"] = fileUsage{bytes: 10, createdAt: now}
	budget.usage.Bytes += 10
	budget.usage.Segments++
	budget.mu.Unlock()
	a.Forget(d, "newest")
	require.InDelta(t, time.Minute.Seconds(), a.Status()[d].OldestAge.Seconds(), 5)
}

// Status must not rescan every file on each call; the oldest entry is cached
// until the oldest file is removed.
func TestStatusReusesCachedOldest(t *testing.T) {
	a := NewAdmission(AdmissionOpts{})
	d := Destination{Database: "DB", Table: "A"}
	for n := 0; n < 100; n++ {
		a.Restore(fmt.Sprintf("p%d", n), d, 8)
	}

	require.NotZero(t, a.Status()[d].OldestAge)

	budget := a.Table(d)
	budget.mu.Lock()
	cached := budget.oldest
	budget.mu.Unlock()
	require.False(t, cached.IsZero(), "oldest is cached after the first read")

	require.NotZero(t, a.Status()[d].OldestAge)
	budget.mu.Lock()
	require.Equal(t, cached, budget.oldest, "repeat reads reuse the cache")
	budget.mu.Unlock()
}
