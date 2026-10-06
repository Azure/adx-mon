package wal

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWriteOptions(t *testing.T) {
	b := bytes.Repeat([]byte("a"), 100)
	wo := WithSampleMetadata(LogSampleType, 42)
	wo(b)

	st, sc := SampleMetadata(b)
	require.Equal(t, LogSampleType, st)
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
			w, err := NewWAL(WALOpts{
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
			w, err := NewWAL(WALOpts{
				StorageDir: t.TempDir(),
				Index:      NewIndex(),
			})
			require.NoError(t, err)
			require.NoError(t, w.Open(context.Background()))

			w.Write(context.Background(), []byte("1970-01-01T00:00:00.001Z,-414304664621325809,{},1.000000000\n"))
			w.Write(context.Background(), []byte("1970-01-01T00:00:00.002Z,-414304664621325809,{},2.000000000\n"))

			require.True(t, w.Size() > 0)

			path := w.Path()

			require.NoError(t, w.Close())

			seg, err := Open(path)
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
			w, err := NewWAL(WALOpts{
				Prefix:     "Foo",
				StorageDir: dir,
			})
			require.NoError(t, err)
			require.NoError(t, w.Open(context.Background()))
			w.Write(context.Background(), []byte("foo"))
			require.True(t, w.Size() > 0)

			require.NoError(t, w.Close())

			w, err = NewWAL(WALOpts{
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
			w, err := NewWAL(WALOpts{
				Prefix:       "Foo",
				StorageDir:   dir,
				MaxDiskUsage: 10,
			})
			require.NoError(t, err)
			require.NoError(t, w.Open(context.Background()))

			require.NoError(t, w.Write(context.Background(), []byte("foo")))

			require.NoError(t, w.Flush())

			require.Error(t, ErrMaxDiskUsageExceeded, w.Write(context.Background(), []byte(strings.Repeat("a", 100))))
			require.Error(t, ErrMaxDiskUsageExceeded, w.Append(context.Background(), []byte(strings.Repeat("a", 100))))

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
			idx := NewIndex()

			w, err := NewWAL(WALOpts{
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
			idx.Add(SegmentInfo{Prefix: "Foo", Path: w.Path(), Size: 1})

			require.Equal(t, ErrMaxSegmentsExceeded, w.Write(context.Background(), []byte(strings.Repeat("a", 100))))
			require.Error(t, ErrMaxSegmentsExceeded, w.Append(context.Background(), []byte(strings.Repeat("a", 100))))

			require.NoError(t, w.Close())
		})
	}
}

func TestWAL_ActiveSegmentDiskUsageLeak(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWAL(WALOpts{
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
	require.ErrorIs(t, err, ErrMaxDiskUsageExceeded)

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
	wals := make([]*WAL, 0, walCount)
	prefixes := []string{"LeakA", "LeakB", "LeakC"}

	for _, prefix := range prefixes {
		w, err := NewWAL(WALOpts{
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

func newTestWAL(t *testing.T, opts WALOpts) *WAL {
	t.Helper()
	opts.StorageDir = t.TempDir()
	if opts.Prefix == "" {
		opts.Prefix = "db_table"
	}
	w, err := NewWAL(opts)
	require.NoError(t, err)
	require.NoError(t, w.Open(context.Background()))
	t.Cleanup(func() { require.NoError(t, w.Close()) })
	return w
}

func TestWAL_TracksCreatedAtForLazySegment(t *testing.T) {
	w := newTestWAL(t, WALOpts{SegmentMaxAge: time.Hour})
	require.Zero(t, atomic.LoadInt64(&w.segmentCreatedAt))

	require.NoError(t, w.Write(context.Background(), []byte("foo")))

	seg := w.Segment()
	require.NotNil(t, seg)
	require.Equal(t, seg.CreatedAt().UnixNano(), atomic.LoadInt64(&w.segmentCreatedAt))
}

func TestWAL_TracksCreatedAtForLazyAppendSegment(t *testing.T) {
	src := newTestWAL(t, WALOpts{Prefix: "db_src"})
	require.NoError(t, src.Write(context.Background(), []byte("foo")))
	require.NoError(t, src.Flush())
	b, err := os.ReadFile(src.Path())
	require.NoError(t, err)

	w := newTestWAL(t, WALOpts{SegmentMaxAge: time.Hour})
	require.NoError(t, w.Append(context.Background(), b))

	seg := w.Segment()
	require.NotNil(t, seg)
	require.Equal(t, seg.CreatedAt().UnixNano(), atomic.LoadInt64(&w.segmentCreatedAt))
}

func TestWAL_FirstSegmentNotRotatedBeforeMaxAge(t *testing.T) {
	// Regression: the creation time of a lazily created first segment was not tracked so it was always considered
	// expired and rotated on the next check regardless of SegmentMaxAge.
	w := newTestWAL(t, WALOpts{SegmentMaxAge: time.Hour})
	require.NoError(t, w.Write(context.Background(), []byte("foo")))
	path := w.Path()

	require.False(t, w.requiresRotation())
	w.rotateSegmentIfNecessary()
	require.Equal(t, path, w.Path())
	require.Zero(t, w.index.TotalSegments())
}

func TestWAL_SubSecondMaxAge(t *testing.T) {
	const maxAge = 50 * time.Millisecond
	w := newTestWAL(t, WALOpts{SegmentMaxAge: maxAge})
	require.NoError(t, w.Write(context.Background(), []byte("foo")))
	path := w.Path()
	createdAt := w.Segment().CreatedAt()

	if time.Since(createdAt) < maxAge {
		require.False(t, w.requiresRotation())
	}

	time.Sleep(time.Until(createdAt.Add(maxAge)) + 5*time.Millisecond)
	require.True(t, w.requiresRotation())

	w.rotateSegmentIfNecessary()
	require.NotEqual(t, path, w.Path())
	require.Equal(t, 1, w.index.TotalSegments())

	// The replacement segment's age is tracked with sub-second precision.
	seg := w.Segment()
	require.NotNil(t, seg)
	require.Equal(t, seg.CreatedAt().UnixNano(), atomic.LoadInt64(&w.segmentCreatedAt))
}

func TestWAL_NoSegmentIsNotRotated(t *testing.T) {
	w := newTestWAL(t, WALOpts{SegmentMaxAge: time.Nanosecond})

	require.False(t, w.requiresRotation())
	w.rotateSegmentIfNecessary()
	require.Nil(t, w.Segment())
	require.Zero(t, w.index.TotalSegments())
}

func TestWAL_RequiresRotationBySize(t *testing.T) {
	w := newTestWAL(t, WALOpts{SegmentMaxSize: 16})
	require.False(t, w.requiresRotation())

	// The segment header and block framing push the segment past the limit.
	require.NoError(t, w.Write(context.Background(), []byte("foo")))
	require.True(t, w.requiresRotation())
}

func BenchmarkWAL_RequiresRotation(b *testing.B) {
	w, err := NewWAL(WALOpts{StorageDir: b.TempDir(), Prefix: "db_table", SegmentMaxAge: time.Hour, SegmentMaxSize: 1 << 30})
	require.NoError(b, err)
	require.NoError(b, w.Open(context.Background()))
	defer w.Close()
	require.NoError(b, w.Write(context.Background(), []byte("foo")))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.requiresRotation()
	}
}
