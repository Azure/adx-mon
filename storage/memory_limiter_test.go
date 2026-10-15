package storage_test

import (
	"bytes"
	"context"
	"io"
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
