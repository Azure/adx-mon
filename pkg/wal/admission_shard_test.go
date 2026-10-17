package wal

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// Budgets must be stable so writers can cache them, and distinct destinations
// must not share state.
func TestTableBudgetHandlesAreStableAndIsolated(t *testing.T) {
	a := NewAdmission(AdmissionOpts{Enforce: true, Default: Usage{Bytes: 100, Segments: 10}})
	d := Destination{Database: "DB", Table: "A"}

	first := a.Table(d)
	require.Same(t, first, a.Table(d), "repeated lookups return the same budget")

	other := a.Table(Destination{Database: "DB", Table: "B"})
	require.NotSame(t, first, other)

	r, err := first.Reserve("a", 100, 1)
	require.NoError(t, err)
	r.Finish(100)
	r.Release()

	_, err = first.Reserve("a", 1, 0)
	var quota *TableOverloadedError
	require.ErrorAs(t, err, &quota, "exhausted table rejects")

	r, err = other.Reserve("b", 100, 1)
	require.NoError(t, err, "unrelated table is unaffected")
	r.Finish(100)
	r.Release()
}

// Per-table limits resolve once; overrides must still win over the default.
func TestTableBudgetResolvesOverrideOnce(t *testing.T) {
	a := NewAdmission(AdmissionOpts{
		Default:   Usage{Bytes: 50, Segments: 5},
		Overrides: []TableLimit{{Database: "DB", Table: "Big", MaxBytes: 500}},
	})

	require.Equal(t, Usage{Bytes: 500, Segments: 5}, a.Table(Destination{Database: "DB", Table: "Big"}).limit)
	require.Equal(t, Usage{Bytes: 50, Segments: 5}, a.Table(Destination{Database: "DB", Table: "Other"}).limit)
}

// Global capacity is shared across shards and must not be exceeded even when
// many destinations reserve concurrently.
func TestGlobalCapacityHoldsAcrossShards(t *testing.T) {
	a := NewAdmission(AdmissionOpts{Global: Usage{Bytes: 1000, Segments: 100}})

	var wg sync.WaitGroup
	for n := 0; n < 200; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			d := Destination{Database: "DB", Table: fmt.Sprintf("T%d", n%8)}
			r, err := a.Reserve(fmt.Sprintf("p%d", n), d, 10, 1)
			if err == nil {
				r.Finish(10)
				r.Release()
			}
		}(n)
	}
	wg.Wait()

	usage := a.Usage()
	require.LessOrEqual(t, usage.Bytes, int64(1000))
	require.LessOrEqual(t, usage.Segments, int64(100))

	// Totals must agree with the sum of per-table state.
	var sum Usage
	for _, s := range a.Status() {
		sum.Bytes += s.Bytes
		sum.Segments += s.Segments
	}
	require.Equal(t, usage, sum, "global counters match per-table accounting")
}

// A rejected global reservation must not leave the table charged.
func TestGlobalRejectionRollsBackTableUsage(t *testing.T) {
	a := NewAdmission(AdmissionOpts{Global: Usage{Bytes: 10}})
	d := Destination{Database: "DB", Table: "A"}

	_, err := a.Reserve("a", d, 50, 1)
	require.ErrorIs(t, err, ErrMaxDiskUsageExceeded)
	require.Equal(t, Usage{}, a.Usage())
	require.Empty(t, a.Status(), "no residual table charge")

	a2 := NewAdmission(AdmissionOpts{Global: Usage{Segments: 1}})
	r, err := a2.Reserve("a", d, 8, 1)
	require.NoError(t, err)
	r.Finish(8)
	r.Release()

	_, err = a2.Reserve("b", d, 8, 1)
	require.ErrorIs(t, err, ErrMaxSegmentsExceeded)
	require.Equal(t, Usage{Bytes: 8, Segments: 1}, a2.Usage(), "failed segment reservation released its bytes")
}
