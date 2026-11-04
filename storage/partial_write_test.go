package storage_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/storage"
	"github.com/stretchr/testify/require"
)

// newFullStore returns a store whose global budget holds only a few small writes.
func newFullStore(t *testing.T) (*storage.LocalStore, *wal.Admission) {
	t.Helper()
	a := wal.NewAdmission(wal.AdmissionOpts{Enforce: true, Global: wal.Usage{Bytes: 2000, Segments: 1000}})
	s := storage.NewLocalStore(storage.StoreOpts{StorageDir: t.TempDir(), Admission: a})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.NoError(t, s.Open(ctx))
	t.Cleanup(func() { s.Close() })
	return s, a
}

// writtenTables returns the number of tables with data in the store.  A segment holding only its 8 byte header has
// no data.
func writtenTables(a *wal.Admission) int {
	n := 0
	for _, u := range a.Snapshot() {
		if u.Bytes > 8 {
			n++
		}
	}
	return n
}

// A remote write request that does not fit in the store is rejected before any series is written, so the sender's
// retry of the whole request does not duplicate the series written before the store filled.
func TestWriteTimeSeriesWritesNothingWhenFull(t *testing.T) {
	s, a := newFullStore(t)
	r := &prompb.WriteRequest{CommonLabels: []*prompb.Label{{Name: []byte("adxmon_database"), Value: []byte("DB")}}}
	for i := 0; i < 50; i++ {
		r.Timeseries = append(r.Timeseries, newTimeSeries(fmt.Sprintf("metric%d", i), nil, 1, 1))
	}

	err := s.WriteTimeSeries(context.Background(), r)
	t.Logf("rejected request left %d of 50 series written: %v", writtenTables(a), err)
	require.ErrorIs(t, err, wal.ErrMaxDiskUsageExceeded)
	require.Zero(t, writtenTables(a), "series written before the store filled would be duplicated by the retry")
}

// A log batch that fills the store reports which logs were not written, so a retry writes only those instead of
// writing the others again.
func TestWriteNativeLogsReportsWhereItStopped(t *testing.T) {
	s, a := newFullStore(t)
	batch := &types.LogBatch{}
	for i := 0; i < 50; i++ {
		log := types.NewLog()
		log.SetAttributeValue(types.AttributeDatabaseName, "DB")
		log.SetAttributeValue(types.AttributeTableName, fmt.Sprintf("Table%d", i))
		log.SetBodyValue("message", "hello")
		batch.Logs = append(batch.Logs, log)
	}

	err := s.WriteNativeLogs(context.Background(), batch)
	require.ErrorIs(t, err, wal.ErrMaxDiskUsageExceeded)
	var partial *storage.PartialWriteError
	require.ErrorAs(t, err, &partial)
	written := 50 - len(partial.Pending)
	t.Logf("store filled after %d of 50 logs", written)
	require.Positive(t, written)
	require.Equal(t, written, writtenTables(a))
}
