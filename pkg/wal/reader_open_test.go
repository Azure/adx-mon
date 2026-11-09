package wal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// A segment whose header can't be read is closed before NewSegmentReader returns its error.
func TestNewSegmentReaderClosesFileOnBadHeader(t *testing.T) {
	fds := func() int {
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Skip("needs /proc/self/fd")
		}
		return len(entries)
	}
	path := filepath.Join(t.TempDir(), "bad.wal")
	require.NoError(t, os.WriteFile(path, []byte("not a segment"), 0o644))
	before := fds()
	for i := 0; i < 20; i++ {
		_, err := NewSegmentReader(path)
		require.ErrorIs(t, err, ErrInvalidWALSegment)
	}
	require.Less(t, fds()-before, 5, "files left open")
}
