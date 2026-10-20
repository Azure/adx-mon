package wal

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSegmentTiming_PartitionsRetries(t *testing.T) {
	base := time.Now()
	at := func(seconds int) time.Time { return base.Add(time.Duration(seconds) * time.Second) }
	timing := NewSegmentTiming(at(0), at(3), false)
	info := SegmentInfo{Timing: timing}
	copy := info
	copy.Timing.Start(at(5), at(7))
	copy.Timing.Release(at(11))
	info.Timing.Start(at(16), at(19))
	got, ok := info.Timing.Complete(at(23))
	require.True(t, ok)
	require.Equal(t, SegmentDurations{
		Total: 23 * time.Second, Buffer: 3 * time.Second, Batching: 2 * time.Second,
		RetryWait: 5 * time.Second, Queue: 5 * time.Second, Processing: 8 * time.Second, Retries: 1,
	}, got)
	require.Equal(t, got.Total, got.Buffer+got.Batching+got.RetryWait+got.Queue+got.Processing)
	timing.Release(at(24))
	_, ok = timing.Complete(at(25))
	require.False(t, ok)
}

func TestSegmentTiming_IncompleteHistory(t *testing.T) {
	base := time.Now()
	ready, queued, started, end := base.Add(time.Second), base.Add(2*time.Second), base.Add(3*time.Second), base.Add(5*time.Second)
	for _, reason := range []string{"recovered", "overlapping_attempts", "missing_enqueue", "missing_start", "invalid_clock"} {
		t.Run(reason, func(t *testing.T) {
			timing := NewSegmentTiming(base, ready, reason == "recovered")
			switch reason {
			case "missing_start":
			case "missing_enqueue":
				timing.Start(time.Time{}, started)
			case "invalid_clock":
				timing.Start(base, started)
			default:
				timing.Start(queued, started)
				if reason == "overlapping_attempts" {
					timing.Start(queued, started)
				}
			}
			got, ok := timing.Complete(end)
			require.True(t, ok)
			require.Equal(t, reason, got.Incomplete)
			require.Equal(t, 5*time.Second, got.Total)
		})
	}
}

func TestSegmentTiming_ConcurrentCompletion(t *testing.T) {
	start := time.Now()
	timing := NewSegmentTiming(start, start, false)
	timing.Start(start, start)
	var completed atomic.Int32
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			if _, ok := timing.Complete(start.Add(time.Second)); ok {
				completed.Add(1)
			}
		})
	}
	wg.Wait()
	require.EqualValues(t, 1, completed.Load())
}

func TestSegmentTiming_MissingCreationTime(t *testing.T) {
	now := time.Now()
	timing := NewSegmentTiming(time.Time{}, now, false)
	timing.Start(now, now)
	got, ok := timing.Complete(now.Add(time.Second))
	require.True(t, ok)
	require.Equal(t, "invalid_clock", got.Incomplete)
	require.Negative(t, got.Total, "unknown creation time must not become a positive lifetime")
}
