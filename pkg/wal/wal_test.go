package wal_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

func TestNewWAL(t *testing.T) {
	tests := []struct {
		Name string
	}{
		{Name: "Disk"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			w, err := wal.NewWAL(wal.WALOpts{
				StorageDir: t.TempDir(),
			})
			require.NoError(t, err)
			require.NoError(t, w.Open(context.Background()))

			w.Write(context.Background(), []byte("foo"))
			w.Write(context.Background(), []byte("foo"))
			w.Write(context.Background(), []byte("foo"))
			require.Equal(t, 1, w.Size())
		})
	}
}

func TestWALSegmentMetadata(t *testing.T) {
	ctx := context.Background()
	prefix := "ADatabase_BTable"
	w, err := wal.NewWAL(wal.WALOpts{
		StorageDir: t.TempDir(),
		Index:      wal.NewIndex(),
		Prefix:     prefix,
	})
	require.NoError(t, err)
	require.NoError(t, w.Open(ctx))

	require.NoError(t, w.Write(ctx, []byte("1970-01-01T00:00:00.001Z,-414304664621325809,{},1.000000000\n"), wal.WithSampleMetadata(wal.MetricSampleType, 1)))
	require.NoError(t, w.Write(ctx, []byte("1970-01-01T00:00:00.002Z,-414304664621325809,{},2.000000000\n"), wal.WithSampleMetadata(wal.MetricSampleType, 1)))

	wpth := w.Path()
	require.NoError(t, w.Flush())
	require.NoError(t, w.Close())

	seg, err := wal.NewSegmentReader(wpth)
	require.NoError(t, err)

	_, err = io.Copy(io.Discard, seg)
	require.NoError(t, err)

	st, sc := seg.Metadata()
	require.Equal(t, wal.MetricSampleType, st)
	require.Equal(t, uint16(2), sc)
}

func TestWAL_Segment(t *testing.T) {
	tests := []struct {
		Name string
	}{
		{Name: "Disk"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			w, err := wal.NewWAL(wal.WALOpts{
				StorageDir: t.TempDir(),
				Index:      wal.NewIndex(),
			})
			require.NoError(t, err)
			require.NoError(t, w.Open(context.Background()))

			w.Write(context.Background(), []byte("1970-01-01T00:00:00.001Z,-414304664621325809,{},1.000000000\n"))
			w.Write(context.Background(), []byte("1970-01-01T00:00:00.002Z,-414304664621325809,{},2.000000000\n"))

			require.Equal(t, 1, w.Size())

			path := w.Path()

			require.NoError(t, w.Close())

			seg, err := wal.Open(path)
			require.NoError(t, err)

			b, err := seg.Bytes()
			require.NoError(t, err)
			require.Equal(t, `1970-01-01T00:00:00.001Z,-414304664621325809,{},1.000000000
1970-01-01T00:00:00.002Z,-414304664621325809,{},2.000000000
`, string(b))
		})
	}
}

func TestWAL_Open(t *testing.T) {
	tests := []struct {
		Name string
	}{
		{Name: "Disk"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()
			w, err := wal.NewWAL(wal.WALOpts{
				Prefix:     "Foo",
				StorageDir: dir,
			})
			require.NoError(t, err)
			require.NoError(t, w.Open(context.Background()))
			w.Write(context.Background(), []byte("foo"))
			require.Equal(t, 1, w.Size())

			require.NoError(t, w.Close())

			w, err = wal.NewWAL(wal.WALOpts{
				Prefix:     "Foo",
				StorageDir: dir,
			})
			require.NoError(t, err)
			require.NoError(t, w.Open(context.Background()))
			require.Equal(t, 0, w.Size())

			w.Write(context.Background(), []byte("foo"))
			require.Equal(t, 1, w.Size())
		})
	}
}

func TestWAL_MaxDiskUsage(t *testing.T) {
	tests := []struct {
		Name string
	}{
		{Name: "Disk"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()
			w, err := wal.NewWAL(wal.WALOpts{
				Prefix:       "Foo",
				StorageDir:   dir,
				MaxDiskUsage: 10,
			})
			require.NoError(t, err)
			require.NoError(t, w.Open(context.Background()))

			require.NoError(t, w.Write(context.Background(), []byte("foo")))

			require.NoError(t, w.Flush())

			require.Error(t, wal.ErrMaxDiskUsageExceeded, w.Write(context.Background(), []byte(strings.Repeat("a", 100))))
			require.Error(t, wal.ErrMaxDiskUsageExceeded, w.Append(context.Background(), []byte(strings.Repeat("a", 100))))

			require.NoError(t, w.Close())
		})
	}
}

func TestWAL_MaxSegmentCount(t *testing.T) {
	tests := []struct {
		Name string
	}{
		{Name: "Disk"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()
			idx := wal.NewIndex()

			w, err := wal.NewWAL(wal.WALOpts{
				Prefix:          "Foo",
				StorageDir:      dir,
				SegmentMaxSize:  1,
				MaxSegmentCount: 1,
				Index:           idx,
			})
			require.NoError(t, err)
			require.NoError(t, w.Open(context.Background()))
			require.NoError(t, w.Write(context.Background(), []byte("foo")))
			require.NoError(t, w.Flush())

			// Simulage another WAL adding a segment to the index.  This should cause new writes to exceed the max segment count.
			idx.Add(wal.SegmentInfo{Prefix: "Foo", Path: w.Path(), Size: 1})

			require.Equal(t, wal.ErrMaxSegmentsExceeded, w.Write(context.Background(), []byte(strings.Repeat("a", 100))))
			require.Error(t, wal.ErrMaxSegmentsExceeded, w.Append(context.Background(), []byte(strings.Repeat("a", 100))))

			require.NoError(t, w.Close())
		})
	}
}
