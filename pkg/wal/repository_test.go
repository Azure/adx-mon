package wal

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func createBenchmarkSegments(ctx context.Context, dir string, count int, payload []byte, workers int) error {
	if workers <= 0 {
		workers = 1
	}
	if workers > count {
		workers = count
	}

	g, gctx := errgroup.WithContext(ctx)
	jobs := make(chan int)

	for i := 0; i < workers; i++ {
		g.Go(func() error {
			for idx := range jobs {
				seg, err := NewSegment(dir, fmt.Sprintf("db_t%d", idx))
				if err != nil {
					return err
				}

				if _, err := seg.Write(gctx, payload); err != nil {
					_ = seg.Close()
					return err
				}

				if err := seg.Close(); err != nil {
					return err
				}
			}
			return nil
		})
	}

	g.Go(func() error {
		defer close(jobs)
		for i := 0; i < count; i++ {
			select {
			case <-gctx.Done():
				return gctx.Err()
			case jobs <- i:
			}
		}
		return nil
	})

	return g.Wait()
}

func createBenchmarkSegmentsParallel(b *testing.B, dir string, count int, payload []byte) {
	b.Helper()

	workers := runtime.NumCPU()
	if workers > 50 {
		workers = 50
	}
	require.NoError(b, createBenchmarkSegments(context.Background(), dir, count, payload, workers))
}

func benchmarkPayload(size int) []byte {
	if size <= 0 {
		return []byte("benchmark-data")
	}
	payload := make([]byte, size)
	r := rand.New(rand.NewSource(99))
	_, _ = r.Read(payload)
	return payload
}

func benchmarkRepositoryOpenStartup(b *testing.B, segmentCount int, payload []byte) {
	dir := b.TempDir()
	createBenchmarkSegmentsParallel(b, dir, segmentCount, payload)

	cases := []struct {
		name        string
		concurrency int
	}{
		{name: "concurrency_1", concurrency: 1},
		{name: "concurrency_50", concurrency: 50},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				ctx, cancel := context.WithCancel(context.Background())
				r := NewRepository(RepositoryOpts{
					StorageDir:             dir,
					StartupOpenConcurrency: tc.concurrency,
				})
				require.NoError(b, r.Open(ctx))
				require.NoError(b, r.Close())
				cancel()
			}
		})
	}
}

// For stable runs on large fixture sets, invoke these with `-benchtime=1x -count=1`.
func BenchmarkRepository_Open_StartupConcurrency(b *testing.B) {
	benchmarkRepositoryOpenStartup(b, 8000, benchmarkPayload(0))
}

func BenchmarkRepository_Open_StartupConcurrency_10MiB(b *testing.B) {
	benchmarkRepositoryOpenStartup(b, 8000, benchmarkPayload(10*1024*1024))
}

func TestRepository_Write(t *testing.T) {
	var providerTests = []struct {
		Name string
	}{
		{Name: "Disk"},
	}
	for _, tt := range providerTests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()
			r := NewRepository(RepositoryOpts{
				StorageDir: dir,
			})
			defer r.Close()

			require.NoError(t, r.Open(context.Background()))
			w, err := r.Get(context.Background(), []byte("foo"))
			require.NoError(t, err)
			require.NoError(t, w.Write(context.Background(), []byte("bar")))
		})
	}
}

func TestRepository_Keys(t *testing.T) {
	var providerTests = []struct {
		Name string
	}{
		{Name: "Disk"},
	}

	for _, tt := range providerTests {
		t.Run(tt.Name, func(t *testing.T) {

			dir := t.TempDir()
			r := NewRepository(RepositoryOpts{
				StorageDir: dir,
			})
			defer r.Close()

			require.NoError(t, r.Open(context.Background()))

			_, err := r.Get(context.Background(), []byte("foo"))
			require.NoError(t, err)

			_, err = r.Get(context.Background(), []byte("foo"))
			require.NoError(t, err)

			_, err = r.Get(context.Background(), []byte("bar"))
			require.NoError(t, err)

			keys := r.Keys()
			require.Equal(t, 2, len(keys))
			require.Equal(t, "bar", string(keys[0]))
			require.Equal(t, "foo", string(keys[1]))
		})
	}
}

func TestRepository_Remove(t *testing.T) {
	var providerTests = []struct {
		Name string
	}{
		{Name: "Disk"},
	}
	for _, tt := range providerTests {
		t.Run(tt.Name, func(t *testing.T) {

			dir := t.TempDir()
			r := NewRepository(RepositoryOpts{
				StorageDir: dir,
			})
			defer r.Close()

			// Add a closed segment for this WAL.
			seg, err := NewSegment(dir, "db_foo")
			n, err := seg.Write(context.Background(), []byte("bar"))
			require.NoError(t, err)
			require.True(t, n > 0)
			require.NoError(t, err)
			require.NoError(t, seg.Close())

			require.NoError(t, r.Open(context.Background()))
			w, err := r.Get(context.Background(), []byte("db_foo"))
			require.NoError(t, err)
			require.NoError(t, w.Write(context.Background(), []byte("bar")))

			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Equal(t, 2, len(entries))

			// Expect an error trying remove WAL that is still open.
			require.Error(t, r.Remove([]byte("db_foo")))

			// WAL must be closed before we can remove it.
			require.NoError(t, w.Close())

			require.NoError(t, r.Remove([]byte("db_foo")))
			require.Equal(t, 0, len(r.Keys()))

			entries, err = os.ReadDir(dir)
			require.NoError(t, err)
			require.Equal(t, 0, len(entries))
		})
	}
}

func TestRepository_WALOptsByPriority(t *testing.T) {
	policy, err := ingestpolicy.New([]ingestpolicy.Table{{Database: "Metrics", Table: "CpuUsage"}})
	require.NoError(t, err)

	r := NewRepository(RepositoryOpts{
		StorageDir:       t.TempDir(),
		SegmentMaxSize:   1024,
		SegmentMaxAge:    time.Minute,
		WALFlushInterval: 100 * time.Millisecond,
		EnableWALFsync:   true,
		Policy:           policy,
		Realtime: RotationPolicy{
			SegmentMaxAge:    250 * time.Millisecond,
			SegmentMaxSize:   512,
			WALFlushInterval: 10 * time.Millisecond,
		},
	})

	for _, prefix := range []string{"Metrics_CpuUsage", "Metrics_CpuUsage_abc123"} {
		opts := r.walOpts(prefix)
		require.Equal(t, ingestpolicy.PriorityRealtime, opts.Priority, prefix)
		require.Equal(t, 250*time.Millisecond, opts.SegmentMaxAge, prefix)
		require.Equal(t, int64(512), opts.SegmentMaxSize, prefix)
		require.Equal(t, 10*time.Millisecond, opts.WALFlushInterval, prefix)
		require.True(t, opts.EnableWALFsync, prefix)
		require.Equal(t, prefix, opts.Prefix)
	}

	for _, prefix := range []string{"Metrics_MemoryUsage_abc123", "Logs_CpuUsage", "invalid"} {
		opts := r.walOpts(prefix)
		require.Equal(t, ingestpolicy.PriorityQueued, opts.Priority, prefix)
		require.Equal(t, time.Minute, opts.SegmentMaxAge, prefix)
		require.Equal(t, int64(1024), opts.SegmentMaxSize, prefix)
		require.Equal(t, 100*time.Millisecond, opts.WALFlushInterval, prefix)
		require.True(t, opts.EnableWALFsync, prefix)
	}
}

func TestRepository_RealtimeInheritsUnsetRotation(t *testing.T) {
	policy, err := ingestpolicy.New([]ingestpolicy.Table{{Database: "Metrics", Table: "CpuUsage"}})
	require.NoError(t, err)

	r := NewRepository(RepositoryOpts{
		StorageDir:       t.TempDir(),
		SegmentMaxSize:   1024,
		SegmentMaxAge:    time.Minute,
		WALFlushInterval: 100 * time.Millisecond,
		Policy:           policy,
		Realtime:         RotationPolicy{SegmentMaxAge: 250 * time.Millisecond},
	})

	opts := r.walOpts("Metrics_CpuUsage_abc123")
	require.Equal(t, ingestpolicy.PriorityRealtime, opts.Priority)
	require.Equal(t, 250*time.Millisecond, opts.SegmentMaxAge)
	require.Equal(t, int64(1024), opts.SegmentMaxSize)
	require.Equal(t, 100*time.Millisecond, opts.WALFlushInterval)
}

func TestRepository_NoPolicyIsQueued(t *testing.T) {
	r := NewRepository(RepositoryOpts{
		StorageDir:    t.TempDir(),
		SegmentMaxAge: time.Minute,
		Realtime:      RotationPolicy{SegmentMaxAge: 250 * time.Millisecond},
	})

	opts := r.walOpts("Metrics_CpuUsage_abc123")
	require.Equal(t, ingestpolicy.PriorityQueued, opts.Priority)
	require.Equal(t, time.Minute, opts.SegmentMaxAge)
}

func TestRepository_GetAssignsPriority(t *testing.T) {
	policy, err := ingestpolicy.New([]ingestpolicy.Table{{Database: "Metrics", Table: "CpuUsage"}})
	require.NoError(t, err)

	r := NewRepository(RepositoryOpts{StorageDir: t.TempDir(), Policy: policy})
	require.NoError(t, r.Open(context.Background()))
	defer r.Close()

	w, err := r.Get(context.Background(), []byte("Metrics_CpuUsage_abc123"))
	require.NoError(t, err)
	require.Equal(t, ingestpolicy.PriorityRealtime, w.Priority())

	w, err = r.Get(context.Background(), []byte("Metrics_MemoryUsage_abc123"))
	require.NoError(t, err)
	require.Equal(t, ingestpolicy.PriorityQueued, w.Priority())
}

func TestRepository_OpenAssignsPriorityToExistingSegments(t *testing.T) {
	dir := t.TempDir()
	seg, err := NewSegment(dir, "Metrics_CpuUsage_abc123")
	require.NoError(t, err)
	_, err = seg.Write(context.Background(), []byte("foo"))
	require.NoError(t, err)
	require.NoError(t, seg.Close())

	policy, err := ingestpolicy.New([]ingestpolicy.Table{{Database: "Metrics", Table: "CpuUsage"}})
	require.NoError(t, err)

	r := NewRepository(RepositoryOpts{StorageDir: dir, Policy: policy})
	require.NoError(t, r.Open(context.Background()))
	defer r.Close()

	w, err := r.Get(context.Background(), []byte("Metrics_CpuUsage_abc123"))
	require.NoError(t, err)
	require.Equal(t, ingestpolicy.PriorityRealtime, w.Priority())
}

func TestRepository_RotatesWALsWithSharedScheduler(t *testing.T) {
	r := NewRepository(RepositoryOpts{StorageDir: t.TempDir(), SegmentMaxAge: 20 * time.Millisecond})
	require.NoError(t, r.Open(context.Background()))
	defer r.Close()

	for i := 0; i < 10; i++ {
		w, err := r.Get(context.Background(), []byte(fmt.Sprintf("db_t%d", i)))
		require.NoError(t, err)
		require.Same(t, r.scheduler, w.scheduler)
		require.False(t, w.ownsScheduler)
		require.NoError(t, w.Write(context.Background(), []byte("foo")))
	}

	require.Eventually(t, func() bool {
		return r.index.TotalSegments() == 10
	}, time.Second, time.Millisecond)
}

func TestRepository_CloseStopsScheduler(t *testing.T) {
	r := NewRepository(RepositoryOpts{StorageDir: t.TempDir(), SegmentMaxAge: time.Hour})
	require.NoError(t, r.Open(context.Background()))

	w, err := r.Get(context.Background(), []byte("db_table"))
	require.NoError(t, err)
	require.NoError(t, w.Write(context.Background(), []byte("foo")))

	done := make(chan struct{})
	go func() {
		require.NoError(t, r.Close())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("repository close did not return")
	}
	_, ok := r.scheduler.next()
	require.False(t, ok)
}

func TestRepository_CloseStopsRotationBeforeClosingWALs(t *testing.T) {
	dir := t.TempDir()
	r := NewRepository(RepositoryOpts{StorageDir: dir, SegmentMaxAge: time.Millisecond})
	require.NoError(t, r.Open(context.Background()))

	for i := 0; i < 10; i++ {
		w, err := r.Get(context.Background(), []byte(fmt.Sprintf("db_t%d", i)))
		require.NoError(t, err)
		require.NoError(t, w.Write(context.Background(), []byte("foo")))
	}
	require.Eventually(t, func() bool { return r.index.TotalSegments() >= 10 }, time.Second, time.Millisecond)

	require.NoError(t, r.Close())
	files, err := os.ReadDir(dir)
	require.NoError(t, err)

	time.Sleep(20 * time.Millisecond)
	after, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Equal(t, files, after)

	r.wals.Each(func(key string, w *WAL) error {
		require.Nil(t, w.Segment(), key)
		return nil
	})
}
