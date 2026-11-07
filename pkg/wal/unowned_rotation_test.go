package wal

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/stretchr/testify/require"
)

// Shortening the age limit applies to the current segment, which rotates at the new limit.
func TestWAL_SetSegmentMaxAgeShortensCurrentSegment(t *testing.T) {
	w := newTestWAL(t, WALOpts{SegmentMaxAge: time.Hour})
	require.NoError(t, w.Write(context.Background(), []byte("foo")))
	require.Equal(t, time.Hour, w.SegmentMaxAge())

	w.SetSegmentMaxAge(50 * time.Millisecond)
	require.Equal(t, 50*time.Millisecond, w.SegmentMaxAge())
	require.Less(t, time.Duration(w.rotationJitter.Load()), 5*time.Millisecond, "jitter follows the new limit")
	require.Eventually(t, func() bool { return w.index.TotalSegments() == 1 }, time.Second, time.Millisecond)
}

// Lengthening the age limit delays the current segment's rotation, and 0 stops rotating it by age.
func TestWAL_SetSegmentMaxAgeLengthensAndDisables(t *testing.T) {
	sched := newRotationScheduler(0, nil)
	w := newTestWAL(t, WALOpts{SegmentMaxAge: time.Second, scheduler: sched})
	require.NoError(t, w.Write(context.Background(), []byte("foo")))
	createdAt := monoTime(atomic.LoadInt64(&w.segmentCreatedAt))

	w.SetSegmentMaxAge(time.Hour)
	deadline, ok := sched.next()
	require.True(t, ok)
	require.False(t, deadline.Before(createdAt.Add(time.Hour-time.Hour/rotationJitterFraction)))
	require.False(t, w.requiresRotation())

	w.SetSegmentMaxAge(0)
	_, ok = sched.next()
	require.False(t, ok)
	require.False(t, w.requiresRotation())
	w.SetSegmentMaxAge(0)
}

func TestRepository_WALOptsForUnownedTables(t *testing.T) {
	policy, err := ingestpolicy.New([]ingestpolicy.Table{{Database: "Metrics", Table: "CpuUsage"}})
	require.NoError(t, err)
	owned := map[string]bool{"Metrics_Owned": true}

	r := NewRepository(RepositoryOpts{
		StorageDir:           t.TempDir(),
		SegmentMaxAge:        time.Minute,
		SegmentMaxSize:       1024,
		Policy:               policy,
		Realtime:             RotationPolicy{SegmentMaxAge: 250 * time.Millisecond},
		UnownedSegmentMaxAge: time.Second,
		Owned:                func(prefix string) bool { return owned[prefix] },
	})

	require.Equal(t, time.Second, r.walOpts("Metrics_Unowned").SegmentMaxAge)
	require.Equal(t, int64(1024), r.walOpts("Metrics_Unowned").SegmentMaxSize, "size limits are unchanged")
	require.Equal(t, time.Minute, r.walOpts("Metrics_Owned").SegmentMaxAge)
	require.Equal(t, 250*time.Millisecond, r.walOpts("Metrics_CpuUsage").SegmentMaxAge, "realtime tables keep their policy")

	// Without an ownership func or an unowned age, every table uses the repository's limit.
	for _, opts := range []RepositoryOpts{
		{StorageDir: t.TempDir(), SegmentMaxAge: time.Minute, UnownedSegmentMaxAge: time.Second},
		{StorageDir: t.TempDir(), SegmentMaxAge: time.Minute, Owned: func(string) bool { return false }},
	} {
		require.Equal(t, time.Minute, NewRepository(opts).walOpts("Metrics_Unowned").SegmentMaxAge)
	}
}

// The sweep applies ownership changes to existing WALs.
func TestRepository_SweepAppliesOwnershipChanges(t *testing.T) {
	var owned atomic.Bool
	owned.Store(true)
	r := NewRepository(RepositoryOpts{
		StorageDir:           t.TempDir(),
		SegmentMaxAge:        time.Hour,
		UnownedSegmentMaxAge: 20 * time.Millisecond,
		Owned:                func(string) bool { return owned.Load() },
	})
	ctx := context.Background()
	require.NoError(t, r.Open(ctx))
	defer r.Close()

	w, err := r.Get(ctx, []byte("db_T"))
	require.NoError(t, err)
	require.NoError(t, w.Write(ctx, []byte("data")))
	require.Equal(t, time.Hour, w.SegmentMaxAge())

	// The table moves to another node: its segment rotates at the unowned limit.
	owned.Store(false)
	r.sweepRotations()
	require.Equal(t, 20*time.Millisecond, w.SegmentMaxAge())
	require.Eventually(t, func() bool { return len(r.Index().Get(nil, "db_T")) == 1 }, time.Second, time.Millisecond)

	// It moves back: segments rotate at the owned limit again.
	owned.Store(true)
	r.sweepRotations()
	require.Equal(t, time.Hour, w.SegmentMaxAge())
	require.NoError(t, w.Write(ctx, []byte("data")))
	time.Sleep(50 * time.Millisecond)
	require.Len(t, r.Index().Get(nil, "db_T"), 1)
}
