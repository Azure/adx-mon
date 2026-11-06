package wal

import (
	"context"
	"encoding/binary"
	"math/rand"
	"os"
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

// An append to a segment whose lock is held returns ErrSegmentLocked so the sender retries later, rather than
// blocking the request.
func TestSegmentAppendReturnsLockedWhileLockHeld(t *testing.T) {
	for name, hold := range map[string]func(*segment) func(){
		"write": func(s *segment) func() { s.mu.Lock(); return s.mu.Unlock },
		"read":  func(s *segment) func() { s.mu.RLock(); return s.mu.RUnlock },
	} {
		t.Run(name, func(t *testing.T) {
			s := newAppendSegment(t)
			b := segmentBytes(t, "a")
			release := hold(s)

			start := time.Now()
			_, err := s.Append(context.Background(), b)
			require.ErrorIs(t, err, ErrSegmentLocked)
			require.Less(t, time.Since(start), time.Second, "the append does not wait for the lock")

			release()
			require.Equal(t, []string(nil), values(t, s), "nothing was written")
			_, err = s.Append(context.Background(), b)
			require.NoError(t, err)
			require.Equal(t, []string{"a"}, values(t, s))
		})
	}
}

// An append is shed while more goroutines than the lock's limit hold or wait for it.
func TestSegmentAppendReturnsLockedAboveWaiterLimit(t *testing.T) {
	s := newAppendSegment(t)
	b := segmentBytes(t, "a")
	release := make(chan struct{})
	done := make(chan struct{})
	const readers = 6
	for i := 0; i < readers; i++ {
		go func() {
			s.mu.RLock()
			<-release
			s.mu.RUnlock()
			done <- struct{}{}
		}()
	}
	require.Eventually(t, func() bool { return s.mu.Waiters() == readers }, time.Second, time.Millisecond)

	_, err := s.Append(context.Background(), b)
	require.ErrorIs(t, err, ErrSegmentLocked)

	close(release)
	for i := 0; i < readers; i++ {
		<-done
	}
	_, err = s.Append(context.Background(), b)
	require.NoError(t, err)
}
