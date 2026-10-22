package storage_test

import (
	"context"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/storage"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMixedMetricShedding(t *testing.T) {
	for _, order := range [][]string{{"bad", "good", "other"}, {"good", "bad", "other"}, {"good", "other", "bad"}} {
		t.Run(order[0]+order[1], func(t *testing.T) {
			a := wal.NewAdmission(wal.AdmissionOpts{Enforce: true, Default: wal.Usage{Bytes: 10000, Segments: 10}, Overrides: []wal.TableLimit{{Database: "DB", Table: "Bad", MaxBytes: 8, MaxSegments: 1}}})
			s := storage.NewLocalStore(storage.StoreOpts{StorageDir: t.TempDir(), Admission: a})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			require.NoError(t, s.Open(ctx))
			defer s.Close()
			r := &prompb.WriteRequest{CommonLabels: []*prompb.Label{{Name: []byte("adxmon_database"), Value: []byte("DB")}}}
			for _, name := range order {
				ts := newTimeSeries(name, nil, 1, 1)
				r.Timeseries = append(r.Timeseries, ts)
			}
			err := s.WriteTimeSeries(ctx, r)
			var outcome *storage.WriteOutcome
			require.ErrorAs(t, err, &outcome)
			require.EqualValues(t, 2, outcome.Accepted)
			require.EqualValues(t, 1, outcome.Rejected)
			require.Len(t, outcome.RejectedSeries, 1)
			require.Greater(t, a.Snapshot()[wal.Destination{Database: "DB", Table: "Good"}].Bytes, int64(8))
		})
	}
}

func TestMixedNativeLogShedding(t *testing.T) {
	a := wal.NewAdmission(wal.AdmissionOpts{Enforce: true, Default: wal.Usage{Bytes: 10000, Segments: 10}, Overrides: []wal.TableLimit{{Database: "DB", Table: "Bad", MaxBytes: 8, MaxSegments: 1}}})
	s := storage.NewLocalStore(storage.StoreOpts{StorageDir: t.TempDir(), Admission: a})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, s.Open(ctx))
	defer s.Close()
	batch := &types.LogBatch{}
	for _, name := range []string{"Bad", "Good", "Bad"} {
		log := types.NewLog()
		log.SetAttributeValue(types.AttributeDatabaseName, "DB")
		log.SetAttributeValue(types.AttributeTableName, name)
		log.SetBodyValue("message", "hello")
		batch.Logs = append(batch.Logs, log)
	}
	var outcome *storage.WriteOutcome
	require.ErrorAs(t, s.WriteNativeLogs(ctx, batch), &outcome)
	require.EqualValues(t, 1, outcome.Accepted)
	require.EqualValues(t, 2, outcome.Rejected)
}

func TestMixedMetricSegmentQuotaShedding(t *testing.T) {
	budget := wal.NewAdmission(wal.AdmissionOpts{Enforce: true, Global: wal.Usage{Bytes: 100000, Segments: 100}, Default: wal.Usage{Bytes: 10000, Segments: 2}})
	s := storage.NewLocalStore(storage.StoreOpts{StorageDir: t.TempDir(), SegmentMaxSize: 1, Admission: budget})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, s.Open(ctx))
	defer s.Close()
	bad := newTimeSeries("bad", map[string]string{"adxmon_database": "DB"}, 1, 1)
	for n := 0; n < 2; n++ {
		require.NoError(t, s.WriteTimeSeries(ctx, &prompb.WriteRequest{Timeseries: []*prompb.TimeSeries{bad}}))
	}
	good := newTimeSeries("good", map[string]string{"adxmon_database": "DB"}, 1, 1)
	var outcome *storage.WriteOutcome
	require.ErrorAs(t, s.WriteTimeSeries(ctx, &prompb.WriteRequest{Timeseries: []*prompb.TimeSeries{bad, good}}), &outcome)
	require.EqualValues(t, 1, outcome.Accepted)
	require.EqualValues(t, 1, outcome.Rejected)
	require.True(t, budget.Status()[wal.Destination{Database: "DB", Table: "Bad"}].Throttled)
	require.EqualValues(t, 2, budget.Status()[wal.Destination{Database: "DB", Table: "Bad"}].Segments)
}
