package wal_test

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

// newReaderSegment returns the path of a closed segment holding the values, each written as its own block.
func newReaderSegment(t *testing.T, values ...string) string {
	t.Helper()
	s, err := wal.NewSegment(t.TempDir(), "db_T")
	require.NoError(t, err)
	for _, v := range values {
		_, err = s.Write(context.Background(), []byte(v), wal.WithSampleMetadata(wal.LogSampleType, 1))
		require.NoError(t, err)
	}
	require.NoError(t, s.Close())
	return s.Path()
}

// After a reader reaches the end of its segment, reads keep returning io.EOF, its path and sample metadata are still
// reported, and it can be closed.
func TestSegmentReader_AfterEOF(t *testing.T) {
	path := newReaderSegment(t, "first,1\n", "second,2\n")
	r, err := wal.NewSegmentReader(path)
	require.NoError(t, err)

	b, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, "first,1\nsecond,2\n", string(b))

	for i := 0; i < 2; i++ {
		n, err := r.Read(make([]byte, 16))
		require.Zero(t, n)
		require.ErrorIs(t, err, io.EOF)
	}
	st, sc := r.SampleMetadata()
	require.Equal(t, wal.LogSampleType, st)
	require.Equal(t, uint32(2), sc)
	require.Equal(t, path, r.Path())
	require.NoError(t, r.Close())
	require.Equal(t, path, r.Path())
}

// Closing a reader twice reports that its file is already closed.
func TestSegmentReader_CloseTwice(t *testing.T) {
	r, err := wal.NewSegmentReader(newReaderSegment(t, "first,1\n"))
	require.NoError(t, err)
	require.NoError(t, r.Close())
	require.ErrorIs(t, r.Close(), os.ErrClosed)
}

// A reader that skips the schema header drops the first line of each block.
func TestSegmentReader_SkipHeaderEachBlock(t *testing.T) {
	r, err := wal.NewSegmentReader(newReaderSegment(t, "h1\na,1\n", "h2\nb,2\n"), wal.WithSkipHeader)
	require.NoError(t, err)
	defer r.Close()
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, "a,1\nb,2\n", string(b))
}

// Readers are read back to back when an upload merges segments.
func TestSegmentReaders_MultiReader(t *testing.T) {
	var readers []io.Reader
	var closers []*wal.SegmentReader
	for _, v := range []string{"a,1\n", "b,2\n", "c,3\n"} {
		r, err := wal.NewSegmentReader(newReaderSegment(t, v))
		require.NoError(t, err)
		readers = append(readers, r)
		closers = append(closers, r)
	}
	b, err := io.ReadAll(io.MultiReader(readers...))
	require.NoError(t, err)
	require.Equal(t, "a,1\nb,2\nc,3\n", string(b))
	for _, r := range closers {
		require.NoError(t, r.Close())
	}
}
