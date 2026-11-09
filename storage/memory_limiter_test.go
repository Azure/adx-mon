package storage_test

import (
	"bytes"
	"context"
	"io"
	"math"
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
	for range 200 {
		wg.Go(func() {
			if m.Reserve(10) {
				m.Release(10)
			}
		})
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
	ctx := t.Context()
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
	ctx := t.Context()
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
	ctx := t.Context()
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

// importUnderCeiling imports a body of size bytes into a store whose intake ceiling is ceiling bytes.
func importUnderCeiling(t *testing.T, ceiling int64, size int) error {
	t.Helper()
	s := storage.NewLocalStore(storage.StoreOpts{StorageDir: t.TempDir(), Admission: wal.NewAdmission(wal.AdmissionOpts{}), MaxIntakeMemory: ceiling})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, s.Open(ctx))
	defer s.Close()
	_, err := s.Import("DB_A_s_1.wal", io.NopCloser(bytes.NewReader(make([]byte, size))))
	require.Zero(t, s.IntakeInFlight(), "an import releases its intake memory when it returns")
	return err
}

// The intake ceiling charges an import for the buffers holding its body, which grow by doubling and briefly hold the
// old copy too.  A 400 KB body grows a 256 KB buffer into a 512 KB one, 768 KB in all, which fits a 1 MB ceiling; a
// 600 KB body grows a 512 KB buffer into a 1 MB one, 1.5 MB in all, which does not.
func TestImportChargesBufferCapacity(t *testing.T) {
	err := importUnderCeiling(t, 1<<20, 400<<10)
	require.False(t, errorsIsExhausted(err), "got %v", err)
	require.ErrorIs(t, importUnderCeiling(t, 1<<20, 600<<10), storage.ErrIntakeMemoryExhausted)
}

// Concurrent imports never hold more buffer memory than the ceiling, however large their bodies.
func TestConcurrentImportsStayWithinIntakeCeiling(t *testing.T) {
	const ceiling = 8 << 20
	s := storage.NewLocalStore(storage.StoreOpts{StorageDir: t.TempDir(), Admission: wal.NewAdmission(wal.AdmissionOpts{}), MaxIntakeMemory: ceiling})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, s.Open(ctx))
	defer s.Close()

	body := make([]byte, 3<<20)
	var peak int64
	var mu sync.Mutex
	stop := make(chan struct{})
	sampled := make(chan struct{})
	go func() {
		defer close(sampled)
		for {
			select {
			case <-stop:
				return
			default:
			}
			mu.Lock()
			peak = max(peak, s.IntakeInFlight())
			mu.Unlock()
		}
	}()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.Import("DB_A_s_1.wal", io.NopCloser(bytes.NewReader(body)))
		}()
	}
	wg.Wait()
	close(stop)
	<-sampled
	require.LessOrEqual(t, peak, int64(ceiling))
	require.Zero(t, s.IntakeInFlight())
}

func TestIntakeNeeded(t *testing.T) {
	require.EqualValues(t, 64<<10, storage.IntakeNeeded(1))
	require.EqualValues(t, 64<<10, storage.IntakeNeeded(64<<10))
	require.EqualValues(t, 192<<10, storage.IntakeNeeded(64<<10+1))
	require.EqualValues(t, 192<<20, storage.IntakeNeeded(100<<20))
	// Sizes too large to buffer saturate instead of overflowing while doubling.
	require.EqualValues(t, int64(math.MaxInt64), storage.IntakeNeeded(1<<62+1))
	require.EqualValues(t, int64(math.MaxInt64), storage.IntakeNeeded(math.MaxInt64))
}

// A body of any size is admitted under a ceiling of IntakeNeeded of its size.
func TestImportAdmittedAtIntakeNeeded(t *testing.T) {
	for _, size := range []int{1, 64 << 10, 64<<10 + 1, 600 << 10, 3 << 20} {
		err := importUnderCeiling(t, storage.IntakeNeeded(int64(size)), size)
		require.False(t, errorsIsExhausted(err), "size %d: got %v", size, err)
	}
}
