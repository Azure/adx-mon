package storage_test

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/storage"
	"github.com/stretchr/testify/require"
)

// newSmallTableBudgetStore returns a store whose tables have an enforced 8 KiB budget, far smaller than a block of
// all of a large request's rows for one table.
func newSmallTableBudgetStore(t *testing.T) (*storage.LocalStore, *wal.Admission) {
	t.Helper()
	a := wal.NewAdmission(wal.AdmissionOpts{Enforce: true, Global: wal.Usage{Bytes: 64 << 20}, Default: wal.Usage{Bytes: 8 << 10}})
	s := storage.NewLocalStore(storage.StoreOpts{StorageDir: t.TempDir(), Admission: a})
	require.NoError(t, s.Open(context.Background()))
	t.Cleanup(func() { s.Close() })
	return s, a
}

// When a table's block of every series is larger than its budget, the series that fit are still written one at a
// time and only the rest are shed, rather than the whole table being shed while the table is empty.
func TestWriteTimeSeriesWritesSeriesThatFitWhenBlockIsTooLarge(t *testing.T) {
	s, a := newSmallTableBudgetStore(t)
	rng := rand.New(rand.NewSource(1))
	req := &prompb.WriteRequest{CommonLabels: []*prompb.Label{{Name: []byte("adxmon_database"), Value: []byte("DB")}}}
	for range 2000 {
		req.Timeseries = append(req.Timeseries, newTimeSeries("cpu", map[string]string{"id": fmt.Sprintf("%016x", rng.Uint64())}, 1, rng.Float64()))
	}

	err := s.WriteTimeSeries(context.Background(), req)
	var outcome *storage.WriteOutcome
	require.ErrorAs(t, err, &outcome)
	t.Logf("2000 series for one table with an 8 KiB budget: accepted=%d rejected=%d", outcome.Accepted, outcome.Rejected)
	require.Positive(t, outcome.Accepted, "the series that fit should be written")
	require.Positive(t, outcome.Rejected)
	require.EqualValues(t, 2000, outcome.Accepted+outcome.Rejected)
	require.Len(t, outcome.RejectedSeries, int(outcome.Rejected))
	require.Positive(t, a.Snapshot()[wal.Destination{Database: "DB", Table: "Cpu"}].Bytes)
}

// The same holds for logs.
func TestWriteNativeLogsWritesLogsThatFitWhenBlockIsTooLarge(t *testing.T) {
	s, _ := newSmallTableBudgetStore(t)
	rng := rand.New(rand.NewSource(1))
	batch := &types.LogBatch{}
	for range 2000 {
		log := types.NewLog()
		log.SetAttributeValue(types.AttributeDatabaseName, "DB")
		log.SetAttributeValue(types.AttributeTableName, "Logs")
		log.SetBodyValue("message", fmt.Sprintf("line %016x", rng.Uint64()))
		batch.Logs = append(batch.Logs, log)
	}

	err := s.WriteNativeLogs(context.Background(), batch)
	var outcome *storage.WriteOutcome
	require.ErrorAs(t, err, &outcome)
	t.Logf("2000 logs for one table with an 8 KiB budget: accepted=%d rejected=%d", outcome.Accepted, outcome.Rejected)
	require.Positive(t, outcome.Accepted, "the logs that fit should be written")
	require.Positive(t, outcome.Rejected)
	require.EqualValues(t, 2000, outcome.Accepted+outcome.Rejected)
}

// fillTable uses all but room bytes of the table's 8 KiB budget, leaving it above the level at which a throttled table
// resumes.
func fillTable(t *testing.T, a *wal.Admission, d wal.Destination, room int64) {
	t.Helper()
	r, err := a.Reserve("filler.wal", d, 8<<10-room, 0)
	require.NoError(t, err)
	r.Finish(8<<10 - room)
}

// When a table is nearly full, a block of all the request's rows for it that does not fit must not throttle the table
// against the rows that do.
func TestWriteTimeSeriesWritesSeriesThatFitInNearlyFullTable(t *testing.T) {
	s, a := newSmallTableBudgetStore(t)
	d := wal.Destination{Database: "DB", Table: "Cpu"}
	fillTable(t, a, d, 996)
	rng := rand.New(rand.NewSource(1))
	req := &prompb.WriteRequest{CommonLabels: []*prompb.Label{{Name: []byte("adxmon_database"), Value: []byte("DB")}}}
	for range 40 {
		req.Timeseries = append(req.Timeseries, newTimeSeries("cpu", map[string]string{"id": fmt.Sprintf("%016x%016x", rng.Uint64(), rng.Uint64())}, 1, rng.Float64()))
	}

	err := s.WriteTimeSeries(context.Background(), req)
	var outcome *storage.WriteOutcome
	require.ErrorAs(t, err, &outcome)
	t.Logf("40 series with 996 bytes left: accepted=%d rejected=%d", outcome.Accepted, outcome.Rejected)
	require.Positive(t, outcome.Accepted, "the series that fit should be written")
	require.EqualValues(t, 40, outcome.Accepted+outcome.Rejected)
}

func TestWriteNativeLogsWritesLogsThatFitInNearlyFullTable(t *testing.T) {
	s, a := newSmallTableBudgetStore(t)
	d := wal.Destination{Database: "DB", Table: "Logs"}
	fillTable(t, a, d, 996)
	rng := rand.New(rand.NewSource(1))
	batch := &types.LogBatch{}
	for range 40 {
		log := types.NewLog()
		log.SetAttributeValue(types.AttributeDatabaseName, "DB")
		log.SetAttributeValue(types.AttributeTableName, "Logs")
		log.SetBodyValue("message", fmt.Sprintf("line %016x%016x", rng.Uint64(), rng.Uint64()))
		batch.Logs = append(batch.Logs, log)
	}

	err := s.WriteNativeLogs(context.Background(), batch)
	var outcome *storage.WriteOutcome
	require.ErrorAs(t, err, &outcome)
	t.Logf("40 logs with 996 bytes left: accepted=%d rejected=%d", outcome.Accepted, outcome.Rejected)
	require.Positive(t, outcome.Accepted, "the logs that fit should be written")
	require.EqualValues(t, 40, outcome.Accepted+outcome.Rejected)
}
