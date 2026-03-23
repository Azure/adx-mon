package wal_test

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"testing"

	"github.com/Azure/adx-mon/pkg/wal"
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
				seg, err := wal.NewSegment(dir, fmt.Sprintf("db_t%d", idx))
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
				r := wal.NewRepository(wal.RepositoryOpts{
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
			r := wal.NewRepository(wal.RepositoryOpts{
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
			r := wal.NewRepository(wal.RepositoryOpts{
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
			r := wal.NewRepository(wal.RepositoryOpts{
				StorageDir: dir,
			})
			defer r.Close()

			// Add a closed segment for this WAL.
			seg, err := wal.NewSegment(dir, "db_foo")
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
