package wal

import (
	"context"
	"encoding/binary"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// segmentBytes returns the bytes of a closed segment holding one block per value, as a collector transfers them.
func segmentBytes(t *testing.T, values ...string) []byte {
	t.Helper()
	s, err := NewSegment(t.TempDir(), "DB_Src")
	require.NoError(t, err)
	for _, v := range values {
		_, err := s.Write(context.Background(), []byte(v))
		require.NoError(t, err)
	}
	require.NoError(t, s.Close())
	b, err := os.ReadFile(s.Path())
	require.NoError(t, err)
	return b
}

func newAppendSegment(t *testing.T, opts ...SegmentOption) *segment {
	t.Helper()
	s, err := NewSegment(t.TempDir(), "DB_Dst", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s.(*segment)
}

// values returns the values of the segment's blocks.
func values(t *testing.T, s *segment) []string {
	t.Helper()
	require.NoError(t, s.Flush())
	iter, err := s.Iterator()
	require.NoError(t, err)
	defer iter.Close()
	var vs []string
	for {
		ok, err := iter.Next()
		require.NoError(t, err)
		if !ok {
			return vs
		}
		vs = append(vs, string(iter.Value()))
	}
}

func TestSegmentAppendWritesBlocks(t *testing.T) {
	s := newAppendSegment(t)
	b := segmentBytes(t, "a", "b")

	n, err := s.Append(context.Background(), b)
	require.NoError(t, err)
	require.Equal(t, len(b)-8, n, "the header is stripped")
	require.EqualValues(t, len(b), s.Size())
	require.Equal(t, []string{"a", "b"}, values(t, s))
}

func TestSegmentAppendIgnoresEmptySegment(t *testing.T) {
	s := newAppendSegment(t)
	n, err := s.Append(context.Background(), segmentBytes(t))
	require.NoError(t, err)
	require.Zero(t, n)
	require.EqualValues(t, 8, s.Size())
}

func TestSegmentAppendRejectsInvalidHeader(t *testing.T) {
	s := newAppendSegment(t)
	_, err := s.Append(context.Background(), []byte("not a segment"))
	require.Error(t, err)
	require.EqualValues(t, 8, s.Size())
}

func TestSegmentAppendRejectsCorruptBlock(t *testing.T) {
	s := newAppendSegment(t)
	b := segmentBytes(t, "a")
	// Corrupt the block's checksum.
	binary.BigEndian.PutUint32(b[12:16], binary.BigEndian.Uint32(b[12:16])+1)

	_, err := s.Append(context.Background(), b)
	require.ErrorContains(t, err, "block checksum verification failed")
	require.EqualValues(t, 8, s.Size())
}

func TestSegmentAppendRejectsClosedSegment(t *testing.T) {
	s := newAppendSegment(t)
	require.NoError(t, s.Close())
	_, err := s.Append(context.Background(), segmentBytes(t, "a"))
	require.ErrorIs(t, err, ErrSegmentClosed)
}

// An append whose blocks do not fit in the table's budget is rejected without writing them.
func TestSegmentAppendRejectsOverBudget(t *testing.T) {
	a := NewAdmission(AdmissionOpts{Enforce: true, Default: Usage{Bytes: 16}})
	s := newAppendSegment(t, WithAdmission(a))

	_, err := s.Append(context.Background(), segmentBytes(t, "a block larger than the budget"))
	var quota *TableOverloadedError
	require.ErrorAs(t, err, &quota)
	require.EqualValues(t, 8, s.Size())
	require.EqualValues(t, 8, a.Snapshot()[Destination{Database: "DB", Table: "Dst"}].Bytes, "the reservation is released")
}

func TestSegmentAppendReturnsWriteError(t *testing.T) {
	a := NewAdmission(AdmissionOpts{})
	s := newAppendSegment(t, WithAdmission(a))
	require.NoError(t, s.w.Close())
	// Larger than the write buffer even when compressed, so it is written through to the closed file.
	big := make([]byte, 4*DefaultIOBufSize)
	_, _ = rand.New(rand.NewSource(1)).Read(big)

	_, err := s.Append(context.Background(), segmentBytes(t, string(big)))
	require.Error(t, err)
	require.EqualValues(t, 8, a.Snapshot()[Destination{Database: "DB", Table: "Dst"}].Bytes, "the reservation is released")
}

// An append to a segment whose lock stays held longer than appendLockWait returns ErrSegmentLocked so the sender
// retries later, rather than blocking the request.
func TestSegmentAppendReturnsLockedWhileLockHeld(t *testing.T) {
	for name, hold := range map[string]func(*segment) func(){
		"write": func(s *segment) func() { s.mu.Lock(); return s.mu.Unlock },
		"read":  func(s *segment) func() { s.mu.RLock(); return s.mu.RUnlock },
	} {
		t.Run(name, func(t *testing.T) {
			s := newAppendSegment(t)
			b := segmentBytes(t, "a")
			release := sync.OnceFunc(hold(s))
			defer release()

			start := time.Now()
			_, err := s.Append(context.Background(), b)
			require.ErrorIs(t, err, ErrSegmentLocked)
			require.GreaterOrEqual(t, time.Since(start), appendLockWait, "the append waits for the lock")
			require.Less(t, time.Since(start), appendLockWait+time.Second, "but no longer than appendLockWait")

			release()
			require.Equal(t, []string(nil), values(t, s), "nothing was written")
			_, err = s.Append(context.Background(), b)
			require.NoError(t, err)
			require.Equal(t, []string{"a"}, values(t, s))
		})
	}
}

// An append is answered locked when readers hold the segment's lock for longer than the append waits, and succeeds
// once they are done.
func TestSegmentAppendReturnsLockedWhileReadersHoldTheLock(t *testing.T) {
	s := newAppendSegment(t)
	b := segmentBytes(t, "a")
	release := make(chan struct{})
	releaseAll := sync.OnceFunc(func() { close(release) })
	defer releaseAll()
	const readers = 6
	var holding, done sync.WaitGroup
	for i := 0; i < readers; i++ {
		holding.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			s.mu.RLock()
			holding.Done()
			<-release
			s.mu.RUnlock()
		}()
	}
	holding.Wait()

	_, err := s.Append(context.Background(), b)
	require.ErrorIs(t, err, ErrSegmentLocked)

	releaseAll()
	done.Wait()
	_, err = s.Append(context.Background(), b)
	require.NoError(t, err)
}

// An append waits for a lock that another writer holds briefly instead of failing.
func TestSegmentAppendWaitsForLockHeldBriefly(t *testing.T) {
	for name, hold := range map[string]func(*segment) func(){
		"write": func(s *segment) func() { s.mu.Lock(); return s.mu.Unlock },
		"read":  func(s *segment) func() { s.mu.RLock(); return s.mu.RUnlock },
	} {
		t.Run(name, func(t *testing.T) {
			s := newAppendSegment(t)
			// Build the payload first so the lock is still held when Append starts, however slow the setup is.
			b := segmentBytes(t, "a")
			release := hold(s)
			time.AfterFunc(appendLockWait/5, release)

			_, err := s.Append(context.Background(), b)
			require.NoError(t, err)
			require.Equal(t, []string{"a"}, values(t, s))
		})
	}
}

// An append stops waiting for the lock when its context is done.
func TestSegmentAppendStopsWaitingWhenContextDone(t *testing.T) {
	s := newAppendSegment(t)
	b := segmentBytes(t, "a")
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(5*time.Millisecond, cancel)

	start := time.Now()
	_, err := s.Append(ctx, b)
	require.ErrorIs(t, err, ErrSegmentLocked)
	require.Less(t, time.Since(start), appendLockWait)
}
