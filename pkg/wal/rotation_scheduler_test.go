// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package wal

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newSchedulerTestWAL() *WAL {
	return &WAL{rotationIndex: -1}
}

func TestRotationScheduler_OrdersByDeadline(t *testing.T) {
	s := newRotationScheduler(0, nil)
	now := time.Now()

	wals := make([]*WAL, 100)
	for i := range wals {
		wals[i] = newSchedulerTestWAL()
	}
	for _, i := range rand.Perm(len(wals)) {
		s.schedule(wals[i], now.Add(time.Duration(i)*time.Millisecond))
	}

	due := s.popDue(nil, now.Add(time.Hour))
	require.Equal(t, wals, due)
	for _, w := range due {
		require.Equal(t, -1, w.rotationIndex)
	}
	_, ok := s.next()
	require.False(t, ok)
}

func TestRotationScheduler_PopDueOnlyReturnsDue(t *testing.T) {
	s := newRotationScheduler(0, nil)
	now := time.Now()
	early, onTime, late := newSchedulerTestWAL(), newSchedulerTestWAL(), newSchedulerTestWAL()
	s.schedule(late, now.Add(time.Second))
	s.schedule(onTime, now)
	s.schedule(early, now.Add(-time.Second))

	require.Equal(t, []*WAL{early, onTime}, s.popDue(nil, now))

	deadline, ok := s.next()
	require.True(t, ok)
	require.Equal(t, now.Add(time.Second), deadline)
}

func TestRotationScheduler_RescheduleReplacesDeadline(t *testing.T) {
	s := newRotationScheduler(0, nil)
	now := time.Now()
	a, b := newSchedulerTestWAL(), newSchedulerTestWAL()
	s.schedule(a, now.Add(time.Second))
	s.schedule(b, now.Add(2*time.Second))

	// Moving b ahead of a must not create a duplicate entry.
	s.schedule(b, now)
	require.Len(t, s.entries, 2)
	deadline, ok := s.next()
	require.True(t, ok)
	require.Equal(t, now, deadline)

	s.schedule(b, now.Add(3*time.Second))
	require.Len(t, s.entries, 2)
	require.Equal(t, []*WAL{a, b}, s.popDue(nil, now.Add(time.Hour)))
}

func TestRotationScheduler_Unschedule(t *testing.T) {
	s := newRotationScheduler(0, nil)
	now := time.Now()
	a, b, c := newSchedulerTestWAL(), newSchedulerTestWAL(), newSchedulerTestWAL()
	s.schedule(a, now)
	s.schedule(b, now.Add(time.Second))
	s.schedule(c, now.Add(2*time.Second))

	s.unschedule(b)
	require.Equal(t, -1, b.rotationIndex)
	// Unscheduling an unscheduled WAL is a no-op.
	s.unschedule(b)

	require.Equal(t, []*WAL{a, c}, s.popDue(nil, now.Add(time.Hour)))
}

func TestRotationScheduler_RunsSweep(t *testing.T) {
	var sweeps atomic.Int32
	s := newRotationScheduler(5*time.Millisecond, func() { sweeps.Add(1) })
	s.Open(context.Background())
	defer s.Close()

	require.Eventually(t, func() bool { return sweeps.Load() >= 3 }, time.Second, time.Millisecond)
}

func TestRotationScheduler_WakesForEarlierDeadline(t *testing.T) {
	// The run loop must re-arm its timer when an earlier deadline is scheduled while it is waiting.
	s := newRotationScheduler(0, nil)
	s.Open(context.Background())
	defer s.Close()

	far := newTestWAL(t, WALOpts{SegmentMaxAge: time.Hour, scheduler: s})
	require.NoError(t, far.Write(context.Background(), []byte("foo")))

	near := newTestWAL(t, WALOpts{SegmentMaxAge: 10 * time.Millisecond, scheduler: s})
	require.NoError(t, near.Write(context.Background(), []byte("foo")))

	require.Eventually(t, func() bool {
		return near.index.TotalSegments() == 1
	}, time.Second, time.Millisecond)
	require.Zero(t, far.index.TotalSegments())
}

func TestRotationScheduler_CloseStopsRotation(t *testing.T) {
	s := newRotationScheduler(0, nil)
	s.Open(context.Background())

	w := newTestWAL(t, WALOpts{SegmentMaxAge: 10 * time.Millisecond, scheduler: s})
	s.Close()
	require.NoError(t, w.Write(context.Background(), []byte("foo")))

	time.Sleep(50 * time.Millisecond)
	require.Zero(t, w.index.TotalSegments())
}

func TestRotationScheduler_ConcurrentSchedule(t *testing.T) {
	s := newRotationScheduler(0, nil)
	s.Open(context.Background())
	defer s.Close()

	wals := make([]*WAL, 50)
	for i := range wals {
		wals[i] = newSchedulerTestWAL()
	}

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(g)))
			for i := 0; i < 1000; i++ {
				w := wals[r.Intn(len(wals))]
				if r.Intn(4) == 0 {
					s.unschedule(w)
				} else {
					s.schedule(w, time.Now().Add(time.Hour+time.Duration(r.Intn(1000))*time.Millisecond))
				}
			}
		}(g)
	}
	wg.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	for i, w := range s.entries {
		require.Equal(t, i, w.rotationIndex)
	}
}

func BenchmarkRotationScheduler_Schedule(b *testing.B) {
	s := newRotationScheduler(0, nil)
	now := time.Now()
	wals := make([]*WAL, 10000)
	for i := range wals {
		wals[i] = newSchedulerTestWAL()
		s.schedule(wals[i], now.Add(time.Duration(i)*time.Millisecond))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := wals[i%len(wals)]
		s.schedule(w, now.Add(time.Duration(i)*time.Microsecond))
	}
}

func TestRotationScheduler_RotatesDueWALsConcurrently(t *testing.T) {
	s := newRotationScheduler(0, nil)
	s.Open(context.Background())
	defer s.Close()

	const n = 200
	wals := make([]*WAL, n)
	for i := range wals {
		wals[i] = newTestWAL(t, WALOpts{Prefix: fmt.Sprintf("db_t%d", i), SegmentMaxAge: 20 * time.Millisecond, scheduler: s})
		require.NoError(t, wals[i].Write(context.Background(), []byte("foo")))
	}

	for _, w := range wals {
		require.Eventually(t, func() bool {
			return w.index.TotalSegments() == 1
		}, 5*time.Second, time.Millisecond)
	}
}
