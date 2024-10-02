package storage_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	logsv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/logs/v1"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/otlp"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/storage"
	"github.com/davecgh/go-spew/spew"
	"github.com/stretchr/testify/require"
)

func TestSeriesKey(t *testing.T) {
	tests := []struct {
		Database []byte
		Labels   []*prompb.Label
		Expect   []byte
	}{
		{
			Labels: newTimeSeries("foo", map[string]string{"adxmon_database": "adxmetrics"}, 0, 0).Labels,
			Expect: []byte("adxmetrics_Foo"),
		},
		{
			Labels: newTimeSeries("foo", map[string]string{"adxmon_database": "OverrideDB"}, 0, 0).Labels,
			Expect: []byte("OverrideDB_Foo"),
		},
	}
	for _, tt := range tests {
		t.Run(string(tt.Expect), func(t *testing.T) {
			b := make([]byte, 256)
			key, err := storage.SegmentKey(b[:0], tt.Labels, 0)
			require.NoError(t, err)
			require.Equal(t, string(tt.Expect), string(key))
		})
	}
}

func TestStore_Open(t *testing.T) {
	b := make([]byte, 256)
	database := "adxmetrics"
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

	ts := newTimeSeries("foo", map[string]string{"adxmon_database": database}, 0, 0)
	key, err := storage.SegmentKey(b[:0], ts.Labels, 0)
	w, err := s.GetWAL(ctx, key)
	require.NoError(t, err)
	require.NotNil(t, w)
	require.NoError(t, s.WriteTimeSeries(context.Background(), []*prompb.TimeSeries{ts}))

	ts = newTimeSeries("foo", map[string]string{"adxmon_database": database}, 1, 1)
	key1, err := storage.SegmentKey(b[:0], ts.Labels, 0)
	w, err = s.GetWAL(ctx, key1)
	require.NoError(t, err)
	require.NotNil(t, w)
	require.NoError(t, s.WriteTimeSeries(context.Background(), []*prompb.TimeSeries{ts}))

	ts = newTimeSeries("bar", map[string]string{"adxmon_database": database}, 0, 0)
	key2, err := storage.SegmentKey(b[:0], ts.Labels, 0)
	w, err = s.GetWAL(ctx, key2)
	require.NoError(t, err)
	require.NotNil(t, w)
	require.NoError(t, s.WriteTimeSeries(context.Background(), []*prompb.TimeSeries{ts}))

	path := w.Path()

	require.Equal(t, 2, s.WALCount())
	require.NoError(t, s.Close())

	r, err := wal.NewSegmentReader(path)
	require.NoError(t, err)
	_, err = io.ReadAll(r)
	require.NoError(t, err)

	s = storage.NewLocalStore(storage.StoreOpts{
		StorageDir:     dir,
		SegmentMaxSize: 1024,
		SegmentMaxAge:  time.Minute,
	})

	require.NoError(t, s.Open(context.Background()))
	defer s.Close()
	require.Equal(t, 2, s.WALCount())
}

func TestStore_WriteTimeSeries(t *testing.T) {
	b := make([]byte, 256)
	database := "adxmetrics"
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

	ts := newTimeSeries("foo", map[string]string{"adxmon_database": database}, 0, 0)
	key, err := storage.SegmentKey(b[:0], ts.Labels, 0)
	require.NoError(t, err)
	w, err := s.GetWAL(ctx, key)
	require.NoError(t, err)
	require.NotNil(t, w)
	require.NoError(t, s.WriteTimeSeries(context.Background(), []*prompb.TimeSeries{ts}))

	path := w.Path()

	require.Equal(t, 1, s.WALCount())
	require.NoError(t, s.Close())

	b, _ = os.ReadFile(path)

	r, err := wal.NewSegmentReader(path)
	require.NoError(t, err)
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, "Timestamp:datetime,SeriesId:long,Labels:dynamic,Value:real\n1970-01-01T00:00:00Z,-4995763953228126371,\"{}\",0.000000000\n", string(data))
}

func TestStore_WriteOTLPLogs_Empty(t *testing.T) {
	b := make([]byte, 256)
	logger.SetLevel(slog.LevelDebug)
	database := "adxlogs"
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

	require.NoError(t, s.WriteOTLPLogs(ctx, database, "foo", &otlp.Logs{
		Logs: make([]*logsv1.LogRecord, 1),
	}))
	time.Sleep(200 * time.Millisecond)
	require.NoError(t, s.WriteOTLPLogs(ctx, database, "foo", &otlp.Logs{}))

	key := fmt.Appendf(b[:0], "%s_%s", database, "foo")
	w, err := s.GetWAL(ctx, key)
	require.NoError(t, err)
	require.NotNil(t, w)

	path := w.Path()

	require.Equal(t, 1, s.WALCount())
	require.NoError(t, s.Close())

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	b, _ = os.ReadFile(path)

	iter, err := wal.NewSegmentIterator(f)
	require.NoError(t, err)
	n, err := iter.Verify()
	require.NoError(t, err)
	require.Equal(t, 1, n)

	r, err := wal.NewSegmentReader(path)
	require.NoError(t, err)
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	spew.Dump(data)
}

func TestStore_WriteNativeLogs_Empty(t *testing.T) {
	b := make([]byte, 256)
	logger.SetLevel(slog.LevelDebug)
	database := "adxlogs"
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

	require.NoError(t, s.WriteNativeLogs(ctx, &types.LogBatch{
		Logs: []*types.Log{
			{
				Attributes: map[string]any{
					types.AttributeDatabaseName: database,
					types.AttributeTableName:    "foo",
				},
			},
		},
	}))
	time.Sleep(200 * time.Millisecond)
	require.NoError(t, s.WriteNativeLogs(ctx, &types.LogBatch{}))

	key := fmt.Appendf(b[:0], "%s_%s", database, "foo")
	w, err := s.GetWAL(ctx, key)
	require.NoError(t, err)
	require.NotNil(t, w)

	path := w.Path()

	require.Equal(t, 1, s.WALCount())
	require.NoError(t, s.Close())

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	b, _ = os.ReadFile(path)

	iter, err := wal.NewSegmentIterator(f)
	require.NoError(t, err)
	n, err := iter.Verify()
	require.NoError(t, err)
	require.Equal(t, 1, n)

	r, err := wal.NewSegmentReader(path)
	require.NoError(t, err)
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	spew.Dump(data)
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

func TestStore_Import_Partial(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewLocalStore(storage.StoreOpts{
		StorageDir:     dir,
		SegmentMaxSize: 1024 * 1025,
		SegmentMaxAge:  time.Minute,
	})

	n, err := s.Import("Database_Metric_123.wal", io.NopCloser(shortReader{}))
	require.Error(t, err)
	require.Equal(t, 0, n)

	dirs, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Equal(t, 0, len(dirs))
}

func TestStore_Import_Append(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewLocalStore(storage.StoreOpts{
		StorageDir:     dir,
		SegmentMaxSize: 1024 * 1025,
		SegmentMaxAge:  time.Minute,
	})

	seg1, err := wal.NewSegment(dir, "Database_Metric")
	require.NoError(t, err)
	seg1.Write(context.Background(), []byte("foo\n"))
	seg1Path := seg1.Path()
	require.NoError(t, seg1.Close())
	seg1Bytes, err := os.ReadFile(seg1Path)
	require.NoError(t, err)

	seg2, err := wal.NewSegment(dir, "Database_Metric")
	require.NoError(t, err)
	seg2.Write(context.Background(), []byte("bar\n"))
	seg2Path := seg2.Path()
	require.NoError(t, seg2.Close())
	seg2Bytes, err := os.ReadFile(seg2Path)
	require.NoError(t, err)

	s1, err := os.Open(seg1Path)
	require.NoError(t, err)
	defer s1.Close()

	s2, err := os.Open(seg2Path)
	require.NoError(t, err)
	defer s2.Close()

	n, err := s.Import("Database_Metric_123.wal", s1)
	require.NoError(t, err)
	require.Equal(t, len(seg1Bytes), n)

	n, err = s.Import("Database_Metric_123.wal", s2)
	require.NoError(t, err)
	require.Equal(t, len(seg2Bytes), n)

	dirs, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Equal(t, 3, len(dirs))

	activeSeg, err := s.GetWAL(context.Background(), []byte("Database_Metric"))
	require.NoError(t, err)
	activeSegPath := activeSeg.Path()

	require.NoError(t, s.Close())

	r, err := wal.NewSegmentReader(activeSegPath)
	require.NoError(t, err)
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, "foo\nbar\n", string(data))

}

func BenchmarkSegmentKey(b *testing.B) {
	buf := make([]byte, 256)
	labels := newTimeSeries("foo", map[string]string{"adxmon_database": "adxmetrics"}, 0, 0).Labels
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		storage.SegmentKey(buf[:0], labels, 0)
	}
}

func BenchmarkWriteTimeSeries(b *testing.B) {
	b.ReportAllocs()
	dir := b.TempDir()
	s := storage.NewLocalStore(storage.StoreOpts{
		StorageDir:     dir,
		SegmentMaxSize: 100 * 1024 * 1024,
		SegmentMaxAge:  time.Minute,
	})

	require.NoError(b, s.Open(context.Background()))
	defer s.Close()
	require.Equal(b, 0, s.WALCount())

	batch := make([]*prompb.TimeSeries, 2500)
	for i := 0; i < 2500; i++ {
		batch[i] = newTimeSeries(fmt.Sprintf("metric%d", i%100), nil, 0, 0)
	}
	for i := 0; i < b.N; i++ {
		require.NoError(b, s.WriteTimeSeries(context.Background(), batch))
	}
}

func newTimeSeries(name string, labels map[string]string, ts int64, val float64) *prompb.TimeSeries {
	l := []*prompb.Label{
		{
			Name:  []byte("__name__"),
			Value: []byte(name),
		},
	}
	for k, v := range labels {
		l = append(l, &prompb.Label{Name: []byte(k), Value: []byte(v)})
	}
	sort.Slice(l, func(i, j int) bool {
		return bytes.Compare(l[i].Name, l[j].Name) < 0
	})

	return &prompb.TimeSeries{
		Labels: l,

		Samples: []*prompb.Sample{
			{
				Timestamp: ts,
				Value:     val,
			},
		},
	}
}

type shortReader struct{}

func (s shortReader) Read(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}
