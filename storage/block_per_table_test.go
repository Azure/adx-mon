package storage_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/storage"
	"github.com/stretchr/testify/require"
)

// walBlocks returns the number of blocks and CSV rows in the store's segments.
func walBlocks(t *testing.T, dir string) (blocks, rows int) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.wal"))
	require.NoError(t, err)
	for _, path := range paths {
		f, err := os.Open(path)
		require.NoError(t, err)
		iter, err := wal.NewSegmentIterator(f)
		require.NoError(t, err)
		for {
			ok, err := iter.Next()
			require.NoError(t, err)
			if !ok {
				break
			}
			blocks++
			// Each block starts with a schema header line.
			rows += bytes.Count(iter.Value(), []byte("\n")) - 1
		}
		require.NoError(t, iter.Close())
	}
	return blocks, rows
}

func newBlockStore(t *testing.T) (*storage.LocalStore, string) {
	t.Helper()
	dir := t.TempDir()
	s := storage.NewLocalStore(storage.StoreOpts{StorageDir: dir, SegmentMaxSize: 100 << 20, SegmentMaxAge: time.Hour, MaxDiskUsage: 1 << 30})
	require.NoError(t, s.Open(context.Background()))
	return s, dir
}

// A remote write request is stored as one WAL block per table rather than one per series, each of which is
// compressed, checksummed, written under the segment lock and admitted separately.
func TestWriteTimeSeriesWritesOneBlockPerTable(t *testing.T) {
	s, dir := newBlockStore(t)
	req := &prompb.WriteRequest{}
	for i := 0; i < 2500; i++ {
		req.Timeseries = append(req.Timeseries, newTimeSeries(fmt.Sprintf("metric%d", i%100), map[string]string{"adxmon_database": "DB", "series": fmt.Sprint(i)}, 1, 1))
	}
	require.NoError(t, s.WriteTimeSeries(context.Background(), req))
	require.NoError(t, s.Close())

	blocks, rows := walBlocks(t, dir)
	t.Logf("2500 series in 100 tables stored as %d blocks holding %d rows", blocks, rows)
	require.Equal(t, 100, blocks)
	require.Equal(t, 2500, rows)
}

// A log batch is stored as one WAL block per table rather than one per log.
func TestWriteNativeLogsWritesOneBlockPerTable(t *testing.T) {
	s, dir := newBlockStore(t)
	batch := &types.LogBatch{}
	for i := 0; i < 1000; i++ {
		log := types.NewLog()
		log.SetAttributeValue(types.AttributeDatabaseName, "DB")
		log.SetAttributeValue(types.AttributeTableName, fmt.Sprintf("Table%d", i%10))
		log.SetBodyValue("message", fmt.Sprintf("line %d", i))
		batch.Logs = append(batch.Logs, log)
	}
	require.NoError(t, s.WriteNativeLogs(context.Background(), batch))
	require.NoError(t, s.Close())

	blocks, rows := walBlocks(t, dir)
	t.Logf("1000 logs in 10 tables stored as %d blocks holding %d rows", blocks, rows)
	require.Equal(t, 10, blocks)
	require.Equal(t, 1000, rows)
}

func BenchmarkWriteNativeLogs(b *testing.B) {
	b.ReportAllocs()
	s := storage.NewLocalStore(storage.StoreOpts{StorageDir: b.TempDir(), SegmentMaxSize: 100 << 20, SegmentMaxAge: time.Minute, MaxDiskUsage: 100 << 30})
	require.NoError(b, s.Open(context.Background()))
	defer s.Close()
	batch := &types.LogBatch{}
	for i := 0; i < 2500; i++ {
		log := types.NewLog()
		log.SetAttributeValue(types.AttributeDatabaseName, "DB")
		log.SetAttributeValue(types.AttributeTableName, fmt.Sprintf("Table%d", i%10))
		log.SetBodyValue("message", fmt.Sprintf("line %d", i))
		batch.Logs = append(batch.Logs, log)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		require.NoError(b, s.WriteNativeLogs(context.Background(), batch))
	}
}
