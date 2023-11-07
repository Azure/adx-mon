package wal_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/pkg/wal/file"
	"github.com/stretchr/testify/require"
)

func TestNewWAL(t *testing.T) {
	tests := []struct {
		Name            string
		StorageProvider file.Provider
	}{
		{Name: "Disk", StorageProvider: &file.DiskProvider{}},
		{Name: "Memory", StorageProvider: &file.MemoryProvider{}},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			w, err := wal.NewWAL(wal.WALOpts{
				StorageDir:      t.TempDir(),
				StorageProvider: tt.StorageProvider,
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

func TestWAL_Segment(t *testing.T) {
	tests := []struct {
		Name            string
		StorageProvider file.Provider
	}{
		{Name: "Disk", StorageProvider: &file.DiskProvider{}},
		{Name: "Memory", StorageProvider: &file.MemoryProvider{}},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			w, err := wal.NewWAL(wal.WALOpts{
				StorageDir:      t.TempDir(),
				StorageProvider: tt.StorageProvider,
				Index:           wal.NewIndex(),
			})
			require.NoError(t, err)
			require.NoError(t, w.Open(context.Background()))

			w.Write(context.Background(), []byte("1970-01-01T00:00:00.001Z,-414304664621325809,{},1.000000000\n"))
			w.Write(context.Background(), []byte("1970-01-01T00:00:00.002Z,-414304664621325809,{},2.000000000\n"))

			require.Equal(t, 1, w.Size())

			path := w.Path()

			require.NoError(t, w.Close())

			seg, err := wal.Open(path, tt.StorageProvider)
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
		Name            string
		StorageProvider file.Provider
	}{
		{Name: "Disk", StorageProvider: &file.DiskProvider{}},
		{Name: "Memory", StorageProvider: &file.MemoryProvider{}},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()
			w, err := wal.NewWAL(wal.WALOpts{
				Prefix:          "Foo",
				StorageDir:      dir,
				StorageProvider: tt.StorageProvider,
			})
			require.NoError(t, err)
			require.NoError(t, w.Open(context.Background()))
			w.Write(context.Background(), []byte("foo"))
			require.Equal(t, 1, w.Size())

			require.NoError(t, w.Close())

			w, err = wal.NewWAL(wal.WALOpts{
				Prefix:          "Foo",
				StorageDir:      dir,
				StorageProvider: tt.StorageProvider,
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
		Name            string
		StorageProvider file.Provider
	}{
		{Name: "Disk", StorageProvider: &file.DiskProvider{}},
		{Name: "Memory", StorageProvider: &file.MemoryProvider{}},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()
			w, err := wal.NewWAL(wal.WALOpts{
				Prefix:          "Foo",
				StorageDir:      dir,
				StorageProvider: tt.StorageProvider,
				MaxDiskUsage:    10,
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
		Name            string
		StorageProvider file.Provider
	}{
		{Name: "Disk", StorageProvider: &file.DiskProvider{}},
		{Name: "Memory", StorageProvider: &file.MemoryProvider{}},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()
			idx := wal.NewIndex()

			w, err := wal.NewWAL(wal.WALOpts{
				Prefix:          "Foo",
				StorageDir:      dir,
				StorageProvider: tt.StorageProvider,
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
