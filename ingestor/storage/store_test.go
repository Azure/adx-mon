package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/stretchr/testify/require"
)

func TestStore_Open(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := storage.NewLocalStore(storage.StoreOpts{
		StorageDir:     dir,
		SegmentMaxSize: 1024,
		SegmentMaxAge:  time.Minute,
	})

	require.NoError(t, s.Open(context.Background()))
	defer s.Close()
	require.Equal(t, 0, s.WALCount())

	ts := newTimeSeries("foo", nil, 0, 0)
	wal, err := s.GetWAL(ctx, ts.Labels)
	require.NoError(t, err)
	require.NotNil(t, wal)
	require.NoError(t, wal.Write(context.Background(), []prompb.TimeSeries{ts}))

	ts = newTimeSeries("foo", nil, 1, 1)
	wal, err = s.GetWAL(ctx, ts.Labels)
	require.NoError(t, err)
	require.NotNil(t, wal)
	require.NoError(t, wal.Write(context.Background(), []prompb.TimeSeries{ts}))

	ts = newTimeSeries("bar", nil, 0, 0)
	wal, err = s.GetWAL(ctx, ts.Labels)
	require.NoError(t, err)
	require.NotNil(t, wal)
	require.NoError(t, wal.Write(context.Background(), []prompb.TimeSeries{ts}))

	require.Equal(t, 2, s.WALCount())

	s = storage.NewLocalStore(storage.StoreOpts{
		StorageDir:     dir,
		SegmentMaxSize: 1024,
		SegmentMaxAge:  time.Minute,
	})

	require.NoError(t, s.Open(context.Background()))
	defer s.Close()
	require.Equal(t, 2, s.WALCount())
}

func TestStore_SkipNonCSV(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewLocalStore(storage.StoreOpts{
		StorageDir:     dir,
		SegmentMaxSize: 1024,
		SegmentMaxAge:  time.Minute,
	})

	f, err := os.Create(filepath.Join(dir, "foo.csv.gz.tmp"))
	require.NoError(t, err)
	defer f.Close()

	require.NoError(t, s.Open(context.Background()))
	defer s.Close()
	require.Equal(t, 0, s.WALCount())
}
