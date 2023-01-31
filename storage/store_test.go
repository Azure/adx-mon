package storage_test

import (
	"context"
	"github.com/Azure/adx-mon/prompb"
	"github.com/Azure/adx-mon/storage"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_Open(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewStore(storage.StoreOpts{
		StorageDir:     dir,
		SegmentMaxSize: 1024,
		SegmentMaxAge:  time.Minute,
	})

	require.NoError(t, s.Open())
	defer s.Close()
	require.Equal(t, 0, s.WALCount())

	ts := newTimeSeries("foo", nil, 0, 0)
	wal, err := s.GetWAL(ts.Labels)
	require.NoError(t, err)
	require.NotNil(t, wal)
	require.NoError(t, wal.Write(context.Background(), []prompb.TimeSeries{ts}))

	ts = newTimeSeries("foo", nil, 1, 1)
	wal, err = s.GetWAL(ts.Labels)
	require.NoError(t, err)
	require.NotNil(t, wal)
	require.NoError(t, wal.Write(context.Background(), []prompb.TimeSeries{ts}))

	ts = newTimeSeries("bar", nil, 0, 0)
	wal, err = s.GetWAL(ts.Labels)
	require.NoError(t, err)
	require.NotNil(t, wal)
	require.NoError(t, wal.Write(context.Background(), []prompb.TimeSeries{ts}))

	require.Equal(t, 2, s.WALCount())

	s = storage.NewStore(storage.StoreOpts{
		StorageDir:     dir,
		SegmentMaxSize: 1024,
		SegmentMaxAge:  time.Minute,
	})

	require.NoError(t, s.Open())
	defer s.Close()
	require.Equal(t, 2, s.WALCount())

}

func TestStore_SkipNonCSV(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewStore(storage.StoreOpts{
		StorageDir:     dir,
		SegmentMaxSize: 1024,
		SegmentMaxAge:  time.Minute,
	})

	f, err := os.Create(filepath.Join(dir, "foo.csv.gz.tmp"))
	require.NoError(t, err)
	defer f.Close()

	require.NoError(t, s.Open())
	defer s.Close()
	require.Equal(t, 0, s.WALCount())
}
