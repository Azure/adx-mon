package wal

import (
	"context"
	"os"
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
	require.Zero(t, w.inflightWriteBytes)
}

func TestWALAppendRejectsClosedWAL(t *testing.T) {
	w := newAppendWAL(t, WALOpts{})
	require.NoError(t, w.Close())
	require.ErrorIs(t, w.Append(context.Background(), segmentBytes(t, "a")), ErrSegmentClosed)
}

func TestWALAppendRejectsWhenDiskLimitReached(t *testing.T) {
	w := newAppendWAL(t, WALOpts{MaxDiskUsage: 8})
	require.ErrorIs(t, w.Append(context.Background(), segmentBytes(t, "a")), ErrMaxDiskUsageExceeded)
	require.Zero(t, w.inflightWriteBytes)
}

func TestWALAppendReturnsSegmentCreationError(t *testing.T) {
	dir := t.TempDir()
	w := newAppendWAL(t, WALOpts{StorageDir: dir})
	require.NoError(t, os.RemoveAll(dir))
	require.Error(t, w.Append(context.Background(), segmentBytes(t, "a")))
}

// An append to a WAL whose active segment is locked returns ErrSegmentLocked at once rather than retrying, so the
// sender can move on and retry later.
func TestWALAppendReturnsLockedWithoutRetrying(t *testing.T) {
	w := newAppendWAL(t, WALOpts{})
	require.NoError(t, w.Append(context.Background(), segmentBytes(t, "a")))
	s := activeSegment(t, w)
	s.mu.Lock()

	start := time.Now()
	err := w.Append(context.Background(), segmentBytes(t, "b"))
	require.ErrorIs(t, err, ErrSegmentLocked)
	require.Less(t, time.Since(start), time.Second)
	require.Zero(t, w.inflightWriteBytes)

	s.mu.Unlock()
	require.Equal(t, []string{"a"}, values(t, s))
}
