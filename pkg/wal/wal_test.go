package wal

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
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

	// The scheduler rotates the segment at its deadline rather than at the periodic sweep.
	require.Eventually(t, func() bool {
		return w.index.TotalSegments() == 1
	}, time.Second, time.Millisecond)
	require.GreaterOrEqual(t, time.Since(createdAt), maxAge)
	require.NotEqual(t, path, w.Path())

	// The replacement segment's age is tracked with sub-second precision.
	seg := w.Segment()
	require.NotNil(t, seg)
	require.Equal(t, seg.CreatedAt().UnixNano(), atomic.LoadInt64(&w.segmentCreatedAt))
}

func TestWAL_RotatesEachSegmentAtMaxAge(t *testing.T) {
	const maxAge = 20 * time.Millisecond
	w := newTestWAL(t, WALOpts{SegmentMaxAge: maxAge})

	for i := 1; i <= 3; i++ {
		require.NoError(t, w.Write(context.Background(), []byte("foo")))
		require.Eventually(t, func() bool {
			return w.index.TotalSegments() == i
		}, time.Second, time.Millisecond)
	}
}

func TestWAL_ClosedWALIsNotRotated(t *testing.T) {
	w, err := NewWAL(WALOpts{StorageDir: t.TempDir(), Prefix: "db_table", SegmentMaxAge: time.Nanosecond})
	require.NoError(t, err)
	require.NoError(t, w.Open(context.Background()))
	require.NoError(t, w.Write(context.Background(), []byte("foo")))
	require.NoError(t, w.Close())

	segments := w.index.TotalSegments()
	w.rotateSegmentIfNecessary()
	require.Nil(t, w.Segment())
	require.Equal(t, segments, w.index.TotalSegments())
	require.Equal(t, -1, w.rotationIndex)
}

func TestWAL_UsesSharedScheduler(t *testing.T) {
	sched := newRotationScheduler(0, nil)
	sched.Open(context.Background())
	defer sched.Close()

	w := newTestWAL(t, WALOpts{SegmentMaxAge: time.Hour, scheduler: sched})
	require.False(t, w.ownsScheduler)
	require.NoError(t, w.Write(context.Background(), []byte("foo")))

	deadline, ok := sched.next()
	require.True(t, ok)
	require.True(t, w.Segment().CreatedAt().Add(time.Hour).Equal(deadline))

	require.NoError(t, w.Close())
	_, ok = sched.next()
	require.False(t, ok)
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

func TestWAL_CloseStopsOwnedSchedulerFirst(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWAL(WALOpts{StorageDir: dir, Prefix: "db_table", SegmentMaxAge: time.Millisecond})
	require.NoError(t, err)
	require.NoError(t, w.Open(context.Background()))
	require.True(t, w.ownsScheduler)

	// Keep the WAL rotating continuously while it is closed.
	require.NoError(t, w.Write(context.Background(), []byte("foo")))
	require.Eventually(t, func() bool { return w.index.TotalSegments() >= 1 }, time.Second, time.Millisecond)

	require.NoError(t, w.Close())
	files, err := os.ReadDir(dir)
	require.NoError(t, err)

	time.Sleep(20 * time.Millisecond)
	after, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Equal(t, files, after)
	require.Nil(t, w.Segment())

	// Close is idempotent.
	require.NoError(t, w.Close())
}

// expireSegment makes the current segment old enough to require rotation.
func expireSegment(w *WAL) {
	atomic.StoreInt64(&w.segmentCreatedAt, time.Now().Add(-2*w.opts.SegmentMaxAge).UnixNano())
}

func walFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestWAL_ActiveRotationCreatesNextSegment(t *testing.T) {
	w := newTestWAL(t, WALOpts{SegmentMaxAge: time.Hour})
	require.NoError(t, w.Write(context.Background(), []byte("foo")))
	path := w.Path()

	expireSegment(w)
	w.rotateSegmentIfNecessary()

	// The rotated segment had data so the next segment is created proactively.
	require.Equal(t, 1, w.index.TotalSegments())
	require.NotNil(t, w.Segment())
	require.NotEqual(t, path, w.Path())
	require.NotZero(t, atomic.LoadInt64(&w.segmentCreatedAt))
}

func TestWAL_InflightWriteCreatesNextSegment(t *testing.T) {
	w := newTestWAL(t, WALOpts{SegmentMaxAge: time.Hour})
	require.NoError(t, w.Write(context.Background(), []byte("foo")))
	expireSegment(w)
	w.rotateSegmentIfNecessary()
	require.NotNil(t, w.Segment())

	// The new segment is empty, but a write is in flight so the WAL is not idle.
	atomic.AddInt64(&w.inflightWriteBytes, 3)
	expireSegment(w)
	w.rotateSegmentIfNecessary()
	atomic.AddInt64(&w.inflightWriteBytes, -3)

	require.NotNil(t, w.Segment())
}

func TestWAL_IdleRotationDoesNotCreateNextSegment(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWAL(WALOpts{StorageDir: dir, Prefix: "db_table", SegmentMaxAge: time.Hour})
	require.NoError(t, err)
	require.NoError(t, w.Open(context.Background()))
	defer w.Close()

	require.NoError(t, w.Write(context.Background(), []byte("foo")))
	expireSegment(w)
	w.rotateSegmentIfNecessary()
	emptyPath := w.Path()
	require.NotEmpty(t, emptyPath)

	// The proactively created segment received no writes for a full period.
	expireSegment(w)
	w.rotateSegmentIfNecessary()

	require.Nil(t, w.Segment())
	require.Zero(t, atomic.LoadInt64(&w.segmentCreatedAt))
	require.Equal(t, -1, w.rotationIndex)
	require.NoFileExists(t, emptyPath)
	require.Equal(t, 1, w.index.TotalSegments())
	require.Len(t, walFiles(t, dir), 1)

	// The next write lazily creates a segment and resumes proactive rotation.
	require.NoError(t, w.Write(context.Background(), []byte("bar")))
	require.NotNil(t, w.Segment())
	require.NotEqual(t, -1, w.rotationIndex)
	expireSegment(w)
	w.rotateSegmentIfNecessary()
	require.Equal(t, 2, w.index.TotalSegments())
	require.NotNil(t, w.Segment())
}

func TestWAL_IdleWALStopsCreatingSegments(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWAL(WALOpts{StorageDir: dir, Prefix: "db_table", SegmentMaxAge: 5 * time.Millisecond})
	require.NoError(t, err)
	require.NoError(t, w.Open(context.Background()))
	defer w.Close()

	require.NoError(t, w.Write(context.Background(), []byte("foo")))
	require.Eventually(t, func() bool { return w.Segment() == nil }, time.Second, time.Millisecond)

	// Only the segment with data remains and no new segments are created while idle.
	files := walFiles(t, dir)
	require.Len(t, files, 1)
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, files, walFiles(t, dir))
	require.Nil(t, w.Segment())
}

func TestWAL_ContinuousWritesKeepProactiveSegment(t *testing.T) {
	w := newTestWAL(t, WALOpts{SegmentMaxAge: 5 * time.Millisecond})

	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		require.NoError(t, w.Write(context.Background(), []byte("foo")))
		time.Sleep(time.Millisecond)
	}
	require.Greater(t, w.index.TotalSegments(), 1)
}

// BenchmarkWAL_WriteDuringRotation measures write latency for concurrent writers while segments rotate frequently.
func BenchmarkWAL_WriteDuringRotation(b *testing.B) {
	for _, maxAge := range []time.Duration{time.Millisecond, 10 * time.Millisecond} {
		b.Run(maxAge.String(), func(b *testing.B) {
			w, err := NewWAL(WALOpts{StorageDir: b.TempDir(), Prefix: "db_table", SegmentMaxAge: maxAge})
			require.NoError(b, err)
			require.NoError(b, w.Open(context.Background()))
			defer w.Close()

			buf := bytes.Repeat([]byte("a"), 256)
			var (
				mu        sync.Mutex
				latencies []time.Duration
			)

			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				local := make([]time.Duration, 0, 1024)
				for pb.Next() {
					start := time.Now()
					if err := w.Write(context.Background(), buf); err != nil {
						b.Error(err)
						return
					}
					local = append(local, time.Since(start))
				}
				mu.Lock()
				latencies = append(latencies, local...)
				mu.Unlock()
			})
			b.StopTimer()

			if len(latencies) == 0 {
				return
			}
			slices.Sort(latencies)
			b.ReportMetric(float64(latencies[len(latencies)*99/100].Nanoseconds()), "p99-ns")
			b.ReportMetric(float64(latencies[len(latencies)-1].Nanoseconds()), "max-ns")
			b.ReportMetric(float64(w.index.TotalSegments())/b.Elapsed().Seconds(), "rotations/s")
		})
	}
}

func TestWAL_WriteAfterSizeRotationKeepsOptions(t *testing.T) {
	w := newTestWAL(t, WALOpts{SegmentMaxSize: 16})
	require.NoError(t, w.Write(context.Background(), []byte("foo")))
	first := w.Path()

	// The segment is over its max size so this write rotates and is retried in a new segment.
	require.NoError(t, w.Write(context.Background(), []byte("bar"), WithSampleMetadata(LogSampleType, 7)))
	second := w.Path()
	require.NotEqual(t, first, second)
	require.NoError(t, w.Close())

	r, err := NewSegmentReader(second)
	require.NoError(t, err)
	defer r.Close()
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, "bar", string(b))

	st, sc := r.SampleMetadata()
	require.Equal(t, LogSampleType, st)
	require.Equal(t, uint32(7), sc)
}

func TestWAL_ConcurrentWritesDuringRotation(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWAL(WALOpts{StorageDir: dir, Prefix: "db_table", SegmentMaxAge: time.Millisecond})
	require.NoError(t, err)
	require.NoError(t, w.Open(context.Background()))

	const (
		writers = 8
		writes  = 2000
	)
	payload := []byte("0123456789\n")

	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < writes; j++ {
				if err := w.Write(context.Background(), payload); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	require.Greater(t, w.index.TotalSegments(), 1)

	// Every write is stored exactly once across all segments.
	var total int
	for _, info := range w.index.Get(nil, "db_table") {
		r, err := NewSegmentReader(info.Path)
		require.NoError(t, err)
		b, err := io.ReadAll(r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
		total += bytes.Count(b, []byte("\n"))
	}
	require.Equal(t, writers*writes, total)
}

func TestWAL_WriteStopsRetryingWhenContextDone(t *testing.T) {
	w := newTestWAL(t, WALOpts{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := 0
	err := w.writeWithRetry(ctx, func() (int, error) {
		calls++
		return 0, ErrSegmentClosed
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)
}

func TestWAL_WriteRetriesClosedSegmentBounded(t *testing.T) {
	w := newTestWAL(t, WALOpts{})

	calls := 0
	err := w.writeWithRetry(context.Background(), func() (int, error) {
		calls++
		return 0, ErrSegmentClosed
	})
	require.ErrorIs(t, err, ErrSegmentClosed)
	require.Equal(t, maxSegmentClosedRetries+1, calls)
}

func TestWAL_WriteRotatesForSizeOnce(t *testing.T) {
	w := newTestWAL(t, WALOpts{})

	calls := 0
	err := w.writeWithRetry(context.Background(), func() (int, error) {
		calls++
		return 0, ErrMaxSegmentSizeExceeded
	})
	require.ErrorIs(t, err, ErrMaxSegmentSizeExceeded)
	require.Equal(t, 2, calls)
}

func TestWAL_ConcurrentRotationDiscardsUnusedSegments(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWAL(WALOpts{StorageDir: dir, Prefix: "db_table", SegmentMaxAge: time.Hour})
	require.NoError(t, err)
	require.NoError(t, w.Open(context.Background()))
	defer w.Close()

	for i := 0; i < 20; i++ {
		require.NoError(t, w.Write(context.Background(), []byte("foo")))
		expireSegment(w)

		start := make(chan struct{})
		var wg sync.WaitGroup
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				w.rotateSegmentIfNecessary()
			}()
		}
		close(start)
		wg.Wait()

		// Exactly one rotation happened and only the closed segments plus the active segment remain on disk.
		require.Equal(t, i+1, w.index.TotalSegments())
		require.NotNil(t, w.Segment())
		require.Len(t, walFiles(t, dir), i+2)
	}
}

func TestDiscardSegment(t *testing.T) {
	discardSegment(nil)

	dir := t.TempDir()
	seg, err := NewSegment(dir, "db_table")
	require.NoError(t, err)
	discardSegment(seg)
	require.NoFileExists(t, seg.Path())
	require.Empty(t, walFiles(t, dir))
}

func TestWAL_ClosedSegmentsCarryPriority(t *testing.T) {
	w := newTestWAL(t, WALOpts{SegmentMaxAge: time.Hour, Priority: ingestpolicy.PriorityRealtime})
	var got []SegmentInfo
	w.index.Subscribe(func(si SegmentInfo) { got = append(got, si) })

	require.NoError(t, w.Write(context.Background(), []byte("foo")))
	path := w.Path()
	expireSegment(w)
	w.rotateSegmentIfNecessary()

	require.Len(t, got, 1)
	require.Equal(t, path, got[0].Path)
	require.Equal(t, "db_table", got[0].Prefix)
	require.Equal(t, ingestpolicy.PriorityRealtime, got[0].Priority)

	// Closing the WAL indexes and notifies the active segment.
	require.NoError(t, w.Write(context.Background(), []byte("bar")))
	path = w.Path()
	require.NoError(t, w.Close())
	require.Len(t, got, 2)
	require.Equal(t, path, got[1].Path)
	require.Equal(t, ingestpolicy.PriorityRealtime, got[1].Priority)
}

func TestWAL_EmptySegmentsAreNotNotified(t *testing.T) {
	w := newTestWAL(t, WALOpts{SegmentMaxAge: time.Hour})
	var got []SegmentInfo
	w.index.Subscribe(func(si SegmentInfo) { got = append(got, si) })

	require.NoError(t, w.Write(context.Background(), []byte("foo")))
	expireSegment(w)
	w.rotateSegmentIfNecessary()
	require.Len(t, got, 1)

	// The proactively created segment is empty when it rotates so it is removed, not indexed.
	expireSegment(w)
	w.rotateSegmentIfNecessary()
	require.Len(t, got, 1)
	require.Equal(t, ingestpolicy.PriorityQueued, got[0].Priority)
}
