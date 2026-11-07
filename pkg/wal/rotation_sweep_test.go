package wal

import (
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/cespare/xxhash"
	"github.com/stretchr/testify/require"
)

// A rotation found by the sweep does not block creating WALs for new tables in the same partition while it closes
// the segment and creates the next one.
func TestRepository_SweepDoesNotBlockNewWALs(t *testing.T) {
	r := NewRepository(RepositoryOpts{StorageDir: t.TempDir(), SegmentMaxSize: 1})
	ctx := context.Background()
	require.NoError(t, r.Open(ctx))
	defer r.Close()

	// A WAL whose segment has exceeded its max size without a later write, so only the sweep rotates it.
	full, err := r.Get(ctx, []byte("db_full"))
	require.NoError(t, err)
	require.NoError(t, full.Write(ctx, []byte("data")))

	release := make(chan struct{})
	testHookSegmentCreated = func() { <-release }
	defer func() { testHookSegmentCreated = nil }()
	swept := make(chan struct{})
	go func() {
		defer close(swept)
		r.sweepRotations()
	}()
	defer func() { close(release); <-swept }()
	time.Sleep(50 * time.Millisecond)

	// A new table in the same partition as the WAL being rotated.
	partition := func(key string) uint64 { return xxhash.Sum64String(key) % 64 }
	key := ""
	for i := 0; key == ""; i++ {
		if k := fmt.Sprintf("db_table%d", i); partition(k) == partition("db_full") {
			key = k
		}
	}
	created := make(chan error, 1)
	go func() {
		_, err := r.Get(ctx, []byte(key))
		created <- err
	}()
	select {
	case err := <-created:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("creating a WAL waited for an unrelated rotation")
	}
}

// WALs whose segments were created together rotate at different times, and never later than their max age.
func TestWAL_RotationDeadlinesAreSpread(t *testing.T) {
	const maxAge = time.Minute
	scheduler := newRotationScheduler(0, nil)
	var deadlines []time.Duration
	for range 50 {
		w := newTestWAL(t, WALOpts{SegmentMaxAge: maxAge, scheduler: scheduler})
		require.NoError(t, w.Write(context.Background(), []byte("data")))
		deadlines = append(deadlines, w.rotationDeadline.Sub(w.Segment().CreatedAt()))
	}

	earliest, latest := deadlines[0], deadlines[0]
	for _, d := range deadlines {
		earliest, latest = min(earliest, d), max(latest, d)
	}
	t.Logf("rotation deadlines of 50 segments created together span %s to %s after creation", earliest, latest)
	require.LessOrEqual(t, latest, maxAge)
	require.GreaterOrEqual(t, latest-earliest, maxAge/20)
}

// The sweep rotates the WALs that are due by their own limits and leaves the others.
func TestRepository_SweepRotatesDueWALs(t *testing.T) {
	r := NewRepository(RepositoryOpts{StorageDir: t.TempDir(), SegmentMaxSize: 64, SegmentMaxAge: time.Hour})
	ctx := context.Background()
	require.NoError(t, r.Open(ctx))
	defer r.Close()

	full, err := r.Get(ctx, []byte("db_full"))
	require.NoError(t, err)
	incompressible := make([]byte, 128)
	_, _ = cryptorand.Read(incompressible)
	require.NoError(t, full.Write(ctx, incompressible))
	small, err := r.Get(ctx, []byte("db_small"))
	require.NoError(t, err)
	require.NoError(t, small.Write(ctx, []byte("data")))
	fullPath, smallPath := full.Path(), small.Path()

	r.sweepRotations()
	require.Len(t, r.Index().Get(nil, "db_full"), 1)
	require.Empty(t, r.Index().Get(nil, "db_small"))
	require.NotEqual(t, fullPath, full.Path())
	require.Equal(t, smallPath, small.Path())
	require.Equal(t, time.Hour, small.opts.SegmentMaxAge)
}
