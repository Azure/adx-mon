package wal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// A table that hits its byte budget must release its active segment so the
// uploader can drain it. Without this the backlog is invisible to the uploader
// whenever the segment has not reached its rotation size or age, and the table
// stays throttled forever.
func TestThrottledTableReleasesSegmentForDraining(t *testing.T) {
	a := NewAdmission(AdmissionOpts{Enforce: true, Default: Usage{Bytes: 200}})

	// Deliberately hostile rotation settings: a segment this large is never
	// rotated by size, and age rotation is disabled.
	w, err := NewWAL(WALOpts{
		StorageDir:     t.TempDir(),
		Prefix:         "DB_A_s",
		SegmentMaxSize: 1 << 30,
		Admission:      a,
	})
	require.NoError(t, err)
	require.NoError(t, w.Open(context.Background()))
	defer w.Close()

	payload := make([]byte, 64)
	var rejected bool
	for n := 0; n < 100 && !rejected; n++ {
		var quota *TableOverloadedError
		if err := w.Write(context.Background(), payload); err != nil {
			require.ErrorAs(t, err, &quota)
			rejected = true
		}
	}
	require.True(t, rejected, "table should reach its budget")

	// The segment holding the backlog must now be closed and indexed.
	require.Nil(t, w.Segment(), "throttled table released its active segment")
	closed := w.index.Get(nil, "DB_A_s")
	require.NotEmpty(t, closed, "backlog is visible to the uploader")

	// Simulate the uploader draining the segment.
	for _, info := range closed {
		require.NoError(t, w.Remove(info.Path))
		w.index.Remove(info)
	}

	require.Equal(t, Usage{}, a.Usage(), "capacity returns after the drain")
	require.NoError(t, w.Write(context.Background(), payload), "table resumes accepting writes")
}

// Releasing the segment must not lose data that was already written.
func TestThrottledReleasePreservesWrittenData(t *testing.T) {
	d := Destination{Database: "DB", Table: "A"}
	a := NewAdmission(AdmissionOpts{Enforce: true, Default: Usage{Bytes: 200}})
	w, err := NewWAL(WALOpts{StorageDir: t.TempDir(), Prefix: "DB_A_s", SegmentMaxSize: 1 << 30, Admission: a})
	require.NoError(t, err)
	require.NoError(t, w.Open(context.Background()))
	defer w.Close()

	payload := make([]byte, 64)
	accepted := 0
	for n := 0; n < 100; n++ {
		if err := w.Write(context.Background(), payload); err != nil {
			break
		}
		accepted++
	}
	require.Positive(t, accepted)

	closed := w.index.Get(nil, "DB_A_s")
	require.NotEmpty(t, closed)

	var total int64
	for _, info := range closed {
		total += info.Size
	}
	require.Greater(t, total, int64(8), "written blocks survived the release")
	require.Equal(t, a.Status()[d].Bytes, total, "charged bytes match the retained segment")
}
