package wal

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// steppedSegment is a segment whose creation time is ahead of the wall clock, as after the clock steps back.
type steppedSegment struct {
	Segment
	createdAt time.Time
}

func (s steppedSegment) CreatedAt() time.Time { return s.createdAt }

// A segment rotates at its max age even when the wall clock steps back after it was created.
func TestWAL_RotationAgeIgnoresWallClockSteps(t *testing.T) {
	const maxAge = 100 * time.Millisecond
	w := newTestWAL(t, WALOpts{SegmentMaxAge: maxAge})
	require.NoError(t, w.Write(context.Background(), []byte("data")))

	w.mu.Lock()
	w.setSegment(steppedSegment{Segment: w.segment, createdAt: time.Now().Add(30 * time.Second)})
	w.mu.Unlock()

	// The WAL's scheduler rotates the segment into the index at its deadline.
	start := time.Now()
	rotated := func() bool { return w.index.TotalSegments() == 1 }
	for deadline := start.Add(time.Second); !rotated() && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if !rotated() {
		t.Fatalf("segment did not rotate within %s of its %s max age after the clock stepped back 30s", time.Since(start).Round(100*time.Millisecond), maxAge)
	}
	t.Logf("segment rotated %s after it became active", time.Since(start).Round(10*time.Millisecond))
}
