package wal

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// The index tracks closed segments; admission owns capacity. Removing a segment
// must release its capacity exactly once, and only through admission.
func TestIndexRemoveReleasesAdmissionOnce(t *testing.T) {
	a := NewAdmission(AdmissionOpts{})
	i := NewIndex()
	i.Admission = a

	d := Destination{Database: "DB", Table: "Table"}
	info := SegmentInfo{Prefix: "DB_Table_a", Path: "a", Size: 10}
	a.Restore(info.Path, d, info.Size)
	i.Add(info)

	require.Equal(t, 1, i.TotalSegments())
	require.EqualValues(t, 10, i.TotalSize())
	require.Equal(t, Usage{Bytes: 10, Segments: 1}, a.Usage())

	i.Remove(info)
	require.Equal(t, 0, i.TotalSegments())
	require.Zero(t, i.TotalSize())
	require.Equal(t, Usage{}, a.Usage())

	// A repeated removal must not double-release.
	i.Remove(info)
	require.Equal(t, Usage{}, a.Usage())
}

// Schema variants of one table share a destination budget while remaining
// distinct index prefixes.
func TestIndexPrefixesShareDestinationBudget(t *testing.T) {
	a := NewAdmission(AdmissionOpts{})
	i := NewIndex()
	i.Admission = a
	d := Destination{Database: "DB", Table: "Table"}

	first := SegmentInfo{Prefix: "DB_Table_a", Path: "a", Size: 10}
	second := SegmentInfo{Prefix: "DB_Table_b", Path: "b", Size: 20}
	a.Restore(first.Path, d, first.Size)
	a.Restore(second.Path, d, second.Size)
	i.Add(first)
	i.Add(second)

	require.Equal(t, Usage{Bytes: 30, Segments: 2}, a.Status()[d].Usage)

	i.Remove(first)
	require.Equal(t, Usage{Bytes: 20, Segments: 1}, a.Status()[d].Usage)
}

func TestIndexConcurrentAddRemove(t *testing.T) {
	a := NewAdmission(AdmissionOpts{})
	i := NewIndex()
	i.Admission = a

	var wg sync.WaitGroup
	for n := 0; n < 100; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s := SegmentInfo{Prefix: "DB_Table_a", Path: fmt.Sprint(n), Size: 10}
			a.Restore(s.Path, Destination{Database: "DB", Table: "Table"}, s.Size)
			i.Add(s)
			i.Remove(s)
		}(n)
	}
	wg.Wait()

	require.Zero(t, i.TotalSize())
	require.Zero(t, i.TotalSegments())
	require.Equal(t, Usage{}, a.Usage())
}
