package storage_test

import (
	"context"
	"testing"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/storage"
	"github.com/stretchr/testify/require"
)

func throttledStore(t *testing.T) (*storage.LocalStore, *wal.Admission, context.CancelFunc) {
	t.Helper()
	budget := wal.NewAdmission(wal.AdmissionOpts{
		Enforce:   true,
		Default:   wal.Usage{Bytes: 100000, Segments: 100},
		Overrides: []wal.TableLimit{{Database: "DB", Table: "Bad", MaxBytes: 8, MaxSegments: 1}},
	})
	s := storage.NewLocalStore(storage.StoreOpts{StorageDir: t.TempDir(), Admission: budget})
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, s.Open(ctx))
	return s, budget, cancel
}

// Once a destination is throttled, further work for it must be rejected before
// a WAL is created for it, so a rejected table cannot accumulate WAL objects
// and goroutines for each schema variant it presents.
func TestThrottledDestinationCreatesNoWALs(t *testing.T) {
	s, _, cancel := throttledStore(t)
	defer cancel()
	defer s.Close()

	req := &prompb.WriteRequest{CommonLabels: []*prompb.Label{{Name: []byte("adxmon_database"), Value: []byte("DB")}}}
	req.Timeseries = []*prompb.TimeSeries{newTimeSeries("bad", nil, 1, 1)}

	// Fill the budget so the destination becomes throttled.
	for n := 0; n < 10; n++ {
		_ = s.WriteTimeSeries(context.Background(), req)
	}
	walsAfterThrottle := s.WALCount()

	// Further writes, including new schema variants, must not create WALs.
	for n := 0; n < 20; n++ {
		err := s.WriteTimeSeries(context.Background(), req)
		var outcome *storage.WriteOutcome
		require.ErrorAs(t, err, &outcome)
		require.EqualValues(t, 1, outcome.Rejected)
	}

	require.Equal(t, walsAfterThrottle, s.WALCount(), "throttled destination creates no further WALs")
}

// The early gate must not affect healthy destinations.
func TestHealthyDestinationStillWritesAfterGate(t *testing.T) {
	s, budget, cancel := throttledStore(t)
	defer cancel()
	defer s.Close()

	req := &prompb.WriteRequest{CommonLabels: []*prompb.Label{{Name: []byte("adxmon_database"), Value: []byte("DB")}}}
	req.Timeseries = []*prompb.TimeSeries{newTimeSeries("bad", nil, 1, 1)}
	for n := 0; n < 10; n++ {
		_ = s.WriteTimeSeries(context.Background(), req)
	}

	healthy := &prompb.WriteRequest{
		CommonLabels: []*prompb.Label{{Name: []byte("adxmon_database"), Value: []byte("DB")}},
		Timeseries:   []*prompb.TimeSeries{newTimeSeries("good", nil, 1, 1)},
	}
	require.NoError(t, s.WriteTimeSeries(context.Background(), healthy))
	require.Positive(t, budget.Status()[wal.Destination{Database: "DB", Table: "Good"}].Bytes)
}

// Native logs take the same early path.
func TestThrottledNativeLogsRejectedEarly(t *testing.T) {
	s, _, cancel := throttledStore(t)
	defer cancel()
	defer s.Close()

	batch := &types.LogBatch{}
	for n := 0; n < 20; n++ {
		log := types.NewLog()
		log.SetAttributeValue(types.AttributeDatabaseName, "DB")
		log.SetAttributeValue(types.AttributeTableName, "Bad")
		log.SetBodyValue("message", "hello")
		batch.Logs = append(batch.Logs, log)
	}

	var outcome *storage.WriteOutcome
	require.ErrorAs(t, s.WriteNativeLogs(context.Background(), batch), &outcome)
	require.Positive(t, outcome.Rejected)
}
