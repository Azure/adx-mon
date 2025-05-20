package wal_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"strings"
	"testing"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

func TestWriteOptions(t *testing.T) {
	b := bytes.Repeat([]byte("a"), 100)
	wo := wal.WithSampleMetadata(wal.LogSampleType, 42)
	wo(b)

	st, sc := wal.SampleMetadata(b)
	require.Equal(t, wal.LogSampleType, st)
	require.Equal(t, uint32(42), sc)
}

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
			require.True(t, w.Size() > 0)
		})
	}
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

			require.True(t, w.Size() > 0)

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
			require.True(t, w.Size() > 0)

			require.NoError(t, w.Close())

			w, err = wal.NewWAL(wal.WALOpts{
				Prefix:     "Foo",
				StorageDir: dir,
			})
			require.NoError(t, err)
			require.NoError(t, w.Open(context.Background()))
			require.Equal(t, 0, w.Size())

			w.Write(context.Background(), []byte("foo"))
			require.True(t, w.Size() > 0)
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
				SegmentMaxSize:  1024,
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

func TestWAL_ActiveSegmentDiskUsageLeak(t *testing.T) {
	dir := t.TempDir()
	w, err := wal.NewWAL(wal.WALOpts{
		Prefix:         "LeakTest",
		StorageDir:     dir,
		MaxDiskUsage:   100,  // 100 bytes max
		SegmentMaxSize: 1000, // Large segment size
	})
	require.NoError(t, err)
	require.NoError(t, w.Open(context.Background()))

	// Write up to just below the max disk usage
	data := make([]byte, 90)
	_, err = rand.Read(data)
	require.NoError(t, err)
	require.NoError(t, w.Write(context.Background(), data))
	require.NoError(t, w.Flush()) // Ensure data is written to disk

	// At this point, the index has no closed segments, so disk usage is 0 in the index
	// But the actual file on disk is larger
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	var total int64
	for _, f := range files {
		fi, err := f.Info()
		require.NoError(t, err)
		total += fi.Size()
	}
	// The file should exist and be nonzero
	require.Greater(t, total, int64(0))

	// Now write more data to exceed MaxDiskUsage, but not trigger rotation
	data2 := make([]byte, 50)
	_, err = rand.Read(data2)
	require.NoError(t, err)
	err = w.Write(context.Background(), data2)
	// This should now fail, since the active segment is counted in disk usage
	require.ErrorIs(t, err, wal.ErrMaxDiskUsageExceeded)

	// Now force a flush and close
	require.NoError(t, w.Flush())
	require.NoError(t, w.Close())

	// After closing, the index should now reflect the segment size
	walsize := int64(0)
	files, err = os.ReadDir(dir)
	require.NoError(t, err)
	for _, f := range files {
		fi, err := f.Info()
		require.NoError(t, err)
		walsize += fi.Size()
	}
	// The total size should now be above MaxDiskUsage
	require.Greater(t, walsize, int64(100))
}

func TestWAL_MultiActiveSegmentsDiskUsageLeak(t *testing.T) {
	dir := t.TempDir()
	maxDisk := int64(100)
	segmentSize := int64(1000)
	walCount := 3
	wals := make([]*wal.WAL, 0, walCount)
	prefixes := []string{"LeakA", "LeakB", "LeakC"}

	for _, prefix := range prefixes {
		w, err := wal.NewWAL(wal.WALOpts{
			Prefix:         prefix,
			StorageDir:     dir,
			MaxDiskUsage:   maxDisk,
			SegmentMaxSize: segmentSize,
		})
		require.NoError(t, err)
		require.NoError(t, w.Open(context.Background()))
		wals = append(wals, w)
	}

	// Write to each WAL so each has a large active segment
	for _, w := range wals {
		data := make([]byte, 90)
		_, err := rand.Read(data)
		require.NoError(t, err)
		require.NoError(t, w.Write(context.Background(), data))
		require.NoError(t, w.Flush()) // Ensure data is written to disk
	}

	// Now, total disk usage should be much greater than maxDisk
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	var total int64
	for _, f := range files {
		fi, err := f.Info()
		require.NoError(t, err)
		total += fi.Size()
	}
	// The total size should be greater than maxDisk
	require.Greater(t, total, maxDisk, "Total disk usage should exceed MaxDiskUsage due to multiple active segments")

	for _, w := range wals {
		require.NoError(t, w.Close())
	}
}
