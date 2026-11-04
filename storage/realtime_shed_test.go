package storage_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/storage"
	"github.com/stretchr/testify/require"
)

// When the queued share of the budget is used up, a request mixing realtime and queued tables writes its realtime
// series and sheds its queued ones, rather than failing whole and holding back the realtime data the reservation
// exists for.
func TestWriteTimeSeriesShedsQueuedSeriesForRealtime(t *testing.T) {
	policy, err := ingestpolicy.New([]ingestpolicy.Table{{Database: "DB", Table: "Fast"}})
	require.NoError(t, err)
	a := wal.NewAdmission(wal.AdmissionOpts{Enforce: true, Global: wal.Usage{Bytes: 64 << 10}, Policy: policy, RealtimeReservedBytes: 32 << 10})
	s := storage.NewLocalStore(storage.StoreOpts{StorageDir: t.TempDir(), Admission: a, Policy: policy})
	ctx := context.Background()
	require.NoError(t, s.Open(ctx))
	defer s.Close()

	labels := []*prompb.Label{{Name: []byte("adxmon_database"), Value: []byte("DB")}}
	// Fill the queued share of the budget.
	for i := 0; ; i++ {
		r := &prompb.WriteRequest{CommonLabels: labels, Timeseries: []*prompb.TimeSeries{newTimeSeries("slow", map[string]string{"n": fmt.Sprint(i)}, 1, 1)}}
		if err := s.WriteTimeSeries(ctx, r); err != nil {
			require.ErrorIs(t, err, wal.ErrMaxDiskUsageExceeded)
			break
		}
	}

	r := &prompb.WriteRequest{CommonLabels: labels}
	for i := 0; i < 5; i++ {
		r.Timeseries = append(r.Timeseries, newTimeSeries("fast", map[string]string{"n": fmt.Sprint(i)}, 1, 1))
		r.Timeseries = append(r.Timeseries, newTimeSeries("slow", map[string]string{"n": fmt.Sprint(i)}, 1, 1))
	}
	fastBefore := a.Snapshot()[wal.Destination{Database: "DB", Table: "Fast"}].Bytes
	err = s.WriteTimeSeries(ctx, r)
	fastWritten := a.Snapshot()[wal.Destination{Database: "DB", Table: "Fast"}].Bytes - fastBefore
	t.Logf("mixed request with the queued share full: err=%v, realtime bytes written=%d", err, fastWritten)

	var outcome *storage.WriteOutcome
	require.ErrorAs(t, err, &outcome, "the request should succeed with the queued series shed")
	require.EqualValues(t, 5, outcome.Accepted)
	require.EqualValues(t, 5, outcome.Rejected)
	require.Positive(t, fastWritten)
}

// When the realtime reservation is used up too, the whole request is rejected before anything is written.
func TestWriteTimeSeriesRejectsWhenRealtimeFull(t *testing.T) {
	policy, err := ingestpolicy.New([]ingestpolicy.Table{{Database: "DB", Table: "Fast"}})
	require.NoError(t, err)
	a := wal.NewAdmission(wal.AdmissionOpts{Enforce: true, Global: wal.Usage{Bytes: 2000}, Policy: policy, RealtimeReservedBytes: 1000})
	s := storage.NewLocalStore(storage.StoreOpts{StorageDir: t.TempDir(), Admission: a, Policy: policy})
	ctx := context.Background()
	require.NoError(t, s.Open(ctx))
	defer s.Close()

	r := &prompb.WriteRequest{CommonLabels: []*prompb.Label{{Name: []byte("adxmon_database"), Value: []byte("DB")}}}
	for i := 0; i < 50; i++ {
		r.Timeseries = append(r.Timeseries, newTimeSeries("fast", map[string]string{"n": fmt.Sprint(i)}, 1, 1))
		r.Timeseries = append(r.Timeseries, newTimeSeries("slow", map[string]string{"n": fmt.Sprint(i)}, 1, 1))
	}
	require.ErrorIs(t, s.WriteTimeSeries(ctx, r), wal.ErrMaxDiskUsageExceeded)
	require.Zero(t, writtenTables(a))
}
