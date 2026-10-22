package wal

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// Batched global accounting must never admit past the configured limit, even
// though per-table movement is published lazily.
func TestGlobalLimitIsNeverExceededWithBatching(t *testing.T) {
	const limit = 1 << 20
	a := NewAdmission(AdmissionOpts{Global: Usage{Bytes: limit}})

	var wg sync.WaitGroup
	for n := 0; n < 64; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			d := Destination{Database: "DB", Table: fmt.Sprintf("T%d", n)}
			for i := 0; i < 128; i++ {
				r, err := a.Reserve(fmt.Sprintf("p%d", n), d, 512, 0)
				if err != nil {
					return
				}
				r.Finish(512)
				r.Release()
			}
		}(n)
	}
	wg.Wait()

	require.LessOrEqual(t, a.Usage().Bytes, int64(limit))
}

// Small limits must stay exact rather than letting concurrent reservations
// overshoot the safety margin.
func TestSmallGlobalLimitStaysExact(t *testing.T) {
	a := NewAdmission(AdmissionOpts{Global: Usage{Bytes: 100}})
	d := Destination{Database: "DB", Table: "A"}

	r, err := a.Reserve("a", d, 100, 0)
	require.NoError(t, err)
	r.Finish(100)
	r.Release()

	_, err = a.Reserve("b", Destination{Database: "DB", Table: "B"}, 1, 0)
	require.ErrorIs(t, err, ErrMaxDiskUsageExceeded)
}

// Freed capacity must become reusable, so published deltas have to settle back
// to the exact total once files drain.
func TestPublishedTotalSettlesAfterDrain(t *testing.T) {
	a := NewAdmission(AdmissionOpts{Global: Usage{Bytes: 1 << 20}})
	d := Destination{Database: "DB", Table: "A"}

	for n := 0; n < 50; n++ {
		path := fmt.Sprintf("p%d", n)
		r, err := a.Reserve(path, d, 1024, 1)
		require.NoError(t, err)
		r.Finish(1024)
		r.Release()
	}
	require.Equal(t, Usage{Bytes: 50 * 1024, Segments: 50}, a.Usage())

	for n := 0; n < 50; n++ {
		a.Forget(d, fmt.Sprintf("p%d", n))
	}

	require.Equal(t, Usage{}, a.Usage())
	require.Zero(t, a.totalBytes.Load(), "global total returns to zero")
	require.Zero(t, a.totalSegments.Load(), "no segment is left charged")
}
