package storage_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/storage"
	"github.com/stretchr/testify/require"
)

func TestMemoryLimiterBoundsConcurrentClaims(t *testing.T) {
	m := storage.NewMemoryLimiter(100)

	require.True(t, m.Reserve(60))
	require.True(t, m.Reserve(40))
	require.False(t, m.Reserve(1), "ceiling is enforced")

	m.Release(40)
	require.True(t, m.Reserve(40), "released capacity is reusable")

	m.Release(60)
	m.Release(40)
	require.Zero(t, m.InFlight())
}

func TestMemoryLimiterIsDisabledWhenUnset(t *testing.T) {
	var m *storage.MemoryLimiter
	require.True(t, m.Reserve(1<<40))
	require.Zero(t, m.InFlight())

	unlimited := storage.NewMemoryLimiter(0)
	require.True(t, unlimited.Reserve(1<<40))
}

func TestMemoryLimiterIsRaceFree(t *testing.T) {
	m := storage.NewMemoryLimiter(1000)
	var wg sync.WaitGroup
	for n := 0; n < 200; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if m.Reserve(10) {
				m.Release(10)
			}
		}()
	}
	wg.Wait()
	require.Zero(t, m.InFlight())
}

// Concurrent imports must not buffer more than the configured ceiling, and the
// rejection must be retryable so the sender keeps its data.
func TestImportRespectsIntakeMemoryCeiling(t *testing.T) {
	budget := wal.NewAdmission(wal.AdmissionOpts{Enforce: true, Default: wal.Usage{Bytes: 1024, Segments: 10}})
	s := storage.NewLocalStore(storage.StoreOpts{
		StorageDir:      t.TempDir(),
		Admission:       budget,
		MaxIntakeMemory: 1024, // room for exactly one in-flight import
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, s.Open(ctx))
	defer s.Close()

	release := make(chan struct{})
	started := make(chan struct{})
	go func() {
		_, _ = s.Import("DB_A_s_1.wal", io.NopCloser(blockingReader{started: started, release: release}))
	}()
	<-started

	_, err := s.Import("DB_B_s_2.wal", io.NopCloser(bytes.NewReader([]byte("data"))))
	require.ErrorIs(t, err, storage.ErrIntakeMemoryExhausted)

	close(release)
	require.Eventually(t, func() bool {
		_, err := s.Import("DB_B_s_3.wal", io.NopCloser(bytes.NewReader([]byte("data"))))
		return err == nil || !errorsIsExhausted(err)
	}, 2*time.Second, 10*time.Millisecond, "capacity is reusable once the first import completes")
}

func errorsIsExhausted(err error) bool {
	return err != nil && err.Error() == storage.ErrIntakeMemoryExhausted.Error()
}

type blockingReader struct {
	started chan struct{}
	release chan struct{}
	done    bool
}

func (r blockingReader) Read(p []byte) (int, error) {
	select {
	case <-r.started:
	default:
		close(r.started)
	}
	<-r.release
	return 0, io.EOF
}

// walPayload returns the bytes of a closed segment holding data, which is what
// a peer transfers.
func walPayload(t *testing.T, data []byte) []byte {
	t.Helper()
	seg, err := wal.NewSegment(t.TempDir(), "DB_A")
	require.NoError(t, err)
	_, err = seg.Write(context.Background(), data)
	require.NoError(t, err)
	path := seg.Path()
	require.NoError(t, seg.Close())
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return b
}

// Memory is charged for the bytes a transfer buffers, not for its destination's
// disk budget, so a small payload is admitted when that budget exceeds the
// intake ceiling.
func TestImportAdmitsPayloadSmallerThanCeilingWhenTableBudgetIsLarger(t *testing.T) {
	payload := walPayload(t, []byte("foo\n"))
	require.Less(t, len(payload), 1024)

	budget := wal.NewAdmission(wal.AdmissionOpts{Enforce: true, Default: wal.Usage{Bytes: 4096, Segments: 10}})
	s := storage.NewLocalStore(storage.StoreOpts{
		StorageDir:      t.TempDir(),
		Admission:       budget,
		MaxIntakeMemory: 1024,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, s.Open(ctx))
	defer s.Close()

	n, err := s.Import("DB_A_s_1.wal", io.NopCloser(bytes.NewReader(payload)))
	require.NoError(t, err)
	require.Equal(t, len(payload), n)
}

// A payload exactly the size of the ceiling fits once the end of the body is
// confirmed; one byte more cannot be buffered and is reported as retryable.
func TestImportAtIntakeCeiling(t *testing.T) {
	payload := walPayload(t, []byte("foo\n"))
	s := storage.NewLocalStore(storage.StoreOpts{
		StorageDir:      t.TempDir(),
		Admission:       wal.NewAdmission(wal.AdmissionOpts{}),
		MaxIntakeMemory: int64(len(payload)),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, s.Open(ctx))
	defer s.Close()

	n, err := s.Import("DB_A_s_1.wal", io.NopCloser(bytes.NewReader(payload)))
	require.NoError(t, err)
	require.Equal(t, len(payload), n)

	_, err = s.Import("DB_A_s_2.wal", io.NopCloser(bytes.NewReader(append(payload, 'x'))))
	require.ErrorIs(t, err, storage.ErrIntakeMemoryExhausted)
}

// Usage is measured without a ceiling so operators can size one from real
// traffic before enabling it.
func TestMemoryLimiterCountsUsageWithoutCeiling(t *testing.T) {
	m := storage.NewMemoryLimiter(0)
	require.True(t, m.Reserve(100))
	require.EqualValues(t, 100, m.InFlight())
	require.Zero(t, m.Limit())
	m.Release(100)
	require.Zero(t, m.InFlight())
}
