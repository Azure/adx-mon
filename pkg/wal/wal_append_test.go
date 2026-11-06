package wal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newAppendWAL(t *testing.T, opts WALOpts) *WAL {
	t.Helper()
	if opts.StorageDir == "" {
		opts.StorageDir = t.TempDir()
	}
	opts.Prefix = "DB_Dst"
	if opts.SegmentMaxAge == 0 {
		opts.SegmentMaxAge = time.Hour
	}
	w, err := NewWAL(opts)
	require.NoError(t, err)
	require.NoError(t, w.Open(context.Background()))
	t.Cleanup(func() { w.Close() })
	return w
}

// activeSegment returns the WAL's active segment.
func activeSegment(t *testing.T, w *WAL) *segment {
	t.Helper()
	w.mu.RLock()
	defer w.mu.RUnlock()
	require.NotNil(t, w.segment)
	return w.segment.(*segment)
}

// The first append creates the WAL's segment, and later appends add to it.
func TestWALAppendCreatesSegment(t *testing.T) {
	w := newAppendWAL(t, WALOpts{})
	w.mu.RLock()
	require.Nil(t, w.segment)
	w.mu.RUnlock()

	require.NoError(t, w.Append(context.Background(), segmentBytes(t, "a")))
	s := activeSegment(t, w)
	require.NoError(t, w.Append(context.Background(), segmentBytes(t, "b")))
	require.Same(t, s, activeSegment(t, w))
	require.Equal(t, []string{"a", "b"}, values(t, s))
	require.Zero(t, w.inflightWriteBytes.Load())
}

func TestWALAppendRejectsClosedWAL(t *testing.T) {
	w := newAppendWAL(t, WALOpts{})
	require.NoError(t, w.Close())
	require.ErrorIs(t, w.Append(context.Background(), segmentBytes(t, "a")), ErrSegmentClosed)
}

func TestWALAppendRejectsWhenDiskLimitReached(t *testing.T) {
	w := newAppendWAL(t, WALOpts{MaxDiskUsage: 8})
	require.ErrorIs(t, w.Append(context.Background(), segmentBytes(t, "a")), ErrMaxDiskUsageExceeded)
	require.Zero(t, w.inflightWriteBytes.Load())
}

func TestWALAppendReturnsSegmentCreationError(t *testing.T) {
	dir := t.TempDir()
	w := newAppendWAL(t, WALOpts{StorageDir: dir})
	require.NoError(t, os.RemoveAll(dir))
	require.Error(t, w.Append(context.Background(), segmentBytes(t, "a")))
}

// An append to a WAL whose active segment stays locked returns ErrSegmentLocked after the segment's bounded wait
// rather than retrying, so the sender can move on and retry later.
func TestWALAppendReturnsLockedWithoutRetrying(t *testing.T) {
	w := newAppendWAL(t, WALOpts{})
	require.NoError(t, w.Append(context.Background(), segmentBytes(t, "a")))
	s := activeSegment(t, w)
	s.mu.Lock()
	unlock := sync.OnceFunc(s.mu.Unlock)
	defer unlock()

	start := time.Now()
	err := w.Append(context.Background(), segmentBytes(t, "b"))
	require.ErrorIs(t, err, ErrSegmentLocked)
	require.Less(t, time.Since(start), appendLockWait+time.Second)
	require.Zero(t, w.inflightWriteBytes.Load())

	unlock()
	require.Equal(t, []string{"a"}, values(t, s))
}

// Many senders appending to one WAL at once all succeed, where failing fast on a held lock shed many of them.
func TestWALAppendConcurrentSendersAllSucceed(t *testing.T) {
	w := newAppendWAL(t, WALOpts{})
	const senders, appends = 50, 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	var locked int
	for i := 0; i < senders; i++ {
		b := segmentBytes(t, fmt.Sprint(i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < appends; j++ {
				if err := w.Append(context.Background(), b); errors.Is(err, ErrSegmentLocked) {
					mu.Lock()
					locked++
					mu.Unlock()
				} else {
					require.NoError(t, err)
				}
			}
		}()
	}
	wg.Wait()
	require.Zero(t, locked)
	require.Len(t, values(t, activeSegment(t, w)), senders*appends)
}
