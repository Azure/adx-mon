package collector

import (
	"context"
	"testing"

	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/storage"
	"github.com/stretchr/testify/require"
)

func TestCollectorWiresTableLimitsWithoutGlobalBackpressure(t *testing.T) {
	svc, err := NewService(&ServiceOpts{StorageDir: t.TempDir(), MaxDiskUsage: 10000, MaxSegmentCount: 100, EnforceTableLimits: true, TableLimits: []wal.TableLimit{{Database: "DB", Table: "Bad", MaxBytes: 8}}})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, svc.store.Open(ctx))
	defer svc.store.Close()
	req := &prompb.WriteRequest{CommonLabels: []*prompb.Label{{Name: []byte("adxmon_database"), Value: []byte("DB")}}}
	for _, name := range []string{"bad", "good"} {
		req.Timeseries = append(req.Timeseries, &prompb.TimeSeries{Labels: []*prompb.Label{{Name: []byte("__name__"), Value: []byte(name)}}, Samples: []*prompb.Sample{{Value: 1, Timestamp: 1}}})
	}
	var outcome *storage.WriteOutcome
	require.ErrorAs(t, svc.store.WriteTimeSeries(ctx, req), &outcome)
	require.EqualValues(t, 1, outcome.Accepted)
	require.EqualValues(t, 1, outcome.Rejected)
	require.True(t, svc.health.IsHealthy())
}
