package wal

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// A reader closes its file and drops its buffers when it reaches the end of the segment, so the readers of a merged
// upload that are done do not hold them while the rest are read.
func TestSegmentReader_ReleasesAtEOF(t *testing.T) {
	s, err := NewSegment(t.TempDir(), "db_T")
	require.NoError(t, err)
	_, err = s.Write(context.Background(), []byte("first,1\n"))
	require.NoError(t, err)
	require.NoError(t, s.Close())

	r, err := NewSegmentReader(s.Path())
	require.NoError(t, err)
	buf := make([]byte, 4)
	_, err = r.Read(buf)
	require.NoError(t, err)
	require.NotNil(t, r.iter, "a reader that is not done keeps its iterator")

	_, err = io.ReadAll(r)
	require.NoError(t, err)
	require.Nil(t, r.iter)
	require.Nil(t, r.buf)
	_, err = r.f.Stat()
	require.ErrorIs(t, err, os.ErrClosed)

	// The caller's Close succeeds once, as for a reader that was not read to its end.
	require.NoError(t, r.Close())
	require.ErrorIs(t, r.Close(), os.ErrClosed)
}
