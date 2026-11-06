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
	a := wal.NewAdmission(wal.AdmissionOpts{Enforce: true, Global: wal.Usage{Bytes: 20000}, Policy: policy, RealtimeReservedBytes: 10000})
	s := storage.NewLocalStore(storage.StoreOpts{StorageDir: t.TempDir(), Admission: a, Policy: policy})
	ctx := context.Background()
	require.NoError(t, s.Open(ctx))
	defer s.Close()
	// Use up all but a little of the realtime reservation.
	fast := wal.Destination{Database: "DB", Table: "Fast"}
	filled, err := a.Reserve("DB_Fast_1.wal", fast, 18500, 0)
	require.NoError(t, err)
	defer filled.Release()

	r := &prompb.WriteRequest{CommonLabels: []*prompb.Label{{Name: []byte("adxmon_database"), Value: []byte("DB")}}}
	for i := 0; i < 50; i++ {
		r.Timeseries = append(r.Timeseries, newTimeSeries("fast", map[string]string{"n": fmt.Sprint(i)}, 1, 1))
		r.Timeseries = append(r.Timeseries, newTimeSeries("slow", map[string]string{"n": fmt.Sprint(i)}, 1, 1))
	}
	require.ErrorIs(t, s.WriteTimeSeries(ctx, r), wal.ErrMaxDiskUsageExceeded)
	require.EqualValues(t, 18500, a.Snapshot()[fast].Bytes, "no realtime series should be written")
	require.LessOrEqual(t, a.Snapshot()[wal.Destination{Database: "DB", Table: "Slow"}].Bytes, int64(8), "no queued series should be written")
}

// A request whose estimate is larger than the whole budget, but whose actual size fits, is written rather than
// rejected on every retry even by an empty store.
func TestWriteTimeSeriesWritesRequestLargerThanEstimateAllows(t *testing.T) {
	a := wal.NewAdmission(wal.AdmissionOpts{Enforce: true, Global: wal.Usage{Bytes: 4096}})
	s := storage.NewLocalStore(storage.StoreOpts{StorageDir: t.TempDir(), Admission: a})
	ctx := context.Background()
	require.NoError(t, s.Open(ctx))
	defer s.Close()

	r := &prompb.WriteRequest{CommonLabels: []*prompb.Label{{Name: []byte("adxmon_database"), Value: []byte("DB")}}}
	for i := 0; i < 60; i++ {
		r.Timeseries = append(r.Timeseries, newTimeSeries("cpu", map[string]string{"n": fmt.Sprint(i)}, 1, 1))
	}
	require.NoError(t, s.WriteTimeSeries(ctx, r))
	used := a.Snapshot()[wal.Destination{Database: "DB", Table: "Cpu"}].Bytes
	t.Logf("60 series estimated above the 4096 byte budget used %d bytes", used)
	require.Positive(t, used)
}

// A large queued payload that is shed must not count against the realtime tables' budget, or it would reject a small
// realtime portion that fits.
func TestWriteTimeSeriesLargeQueuedPayloadDoesNotBlockRealtime(t *testing.T) {
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

	// One realtime series alongside queued series whose estimate alone is larger than the whole budget.
	r := &prompb.WriteRequest{CommonLabels: labels, Timeseries: []*prompb.TimeSeries{newTimeSeries("fast", nil, 1, 1)}}
	for i := 0; i < 1000; i++ {
		r.Timeseries = append(r.Timeseries, newTimeSeries("slow", map[string]string{"n": fmt.Sprint(i)}, 1, 1))
	}
	err = s.WriteTimeSeries(ctx, r)

	var outcome *storage.WriteOutcome
	require.ErrorAs(t, err, &outcome, "the realtime series fits and should be written")
	require.EqualValues(t, 1, outcome.Accepted)
	require.EqualValues(t, 1000, outcome.Rejected)
}
