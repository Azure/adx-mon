package wal

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	require.NoError(t, err)
	return fi.Size()
}

// A segment's size is the bytes it holds on disk: the header and its compressed blocks.
func TestSegmentInfoSizeAfterWrite(t *testing.T) {
	s, err := NewSegment(t.TempDir(), "db_T")
	require.NoError(t, err)
	require.Equal(t, int64(8), s.Info().Size)

	_, err = s.Write(context.Background(), []byte("first,1\n"))
	require.NoError(t, err)
	_, err = s.Write(context.Background(), []byte("second,2\n"))
	require.NoError(t, err)
	info := s.Info()
	require.NoError(t, s.Close())

	require.Equal(t, s.Size(), info.Size)
	require.Equal(t, fileSize(t, s.Path()), info.Size)
}

// Appending a transferred segment adds its blocks, without its header, to the size.
func TestSegmentInfoSizeAfterAppend(t *testing.T) {
	s := newAppendSegment(t)
	transfer := segmentBytes(t, "first,1\n", "second,2\n")
	n, err := s.Append(context.Background(), transfer)
	require.NoError(t, err)
	require.Equal(t, len(transfer)-8, n)
	require.Equal(t, int64(len(transfer)), s.Info().Size)
}

// A segment opened from disk reports its file size, and after a repair the size it was truncated to.
func TestOpenSegmentInfoSize(t *testing.T) {
	s, err := NewSegment(t.TempDir(), "db_T")
	require.NoError(t, err)
	_, err = s.Write(context.Background(), []byte("first,1\n"))
	require.NoError(t, err)
	require.NoError(t, s.Close())
	good := fileSize(t, s.Path())

	opened, err := Open(s.Path())
	require.NoError(t, err)
	require.Equal(t, good, opened.Info().Size)
	require.NoError(t, opened.Close())

	// A corrupt tail is truncated when the segment is opened.
	f, err := os.OpenFile(s.Path(), os.O_APPEND|os.O_WRONLY, 0600)
	require.NoError(t, err)
	_, err = f.Write([]byte{0, 0, 0, 4, 1, 2, 3, 4, 9, 9, 9, 9})
	require.NoError(t, err)
	require.NoError(t, f.Close())

	repaired, err := Open(s.Path())
	require.NoError(t, err)
	require.Equal(t, good, repaired.Info().Size)
	require.NoError(t, repaired.Close())
}

// Segments found on disk at startup are indexed with their file size.
func TestRepositoryIndexesSegmentsFoundAtStartup(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSegment(dir, "db_T")
	require.NoError(t, err)
	_, err = s.Write(context.Background(), []byte("first,1\n"))
	require.NoError(t, err)
	require.NoError(t, s.Close())

	r := NewRepository(RepositoryOpts{StorageDir: dir})
	require.NoError(t, r.Open(context.Background()))
	defer r.Close()

	infos := r.Index().Get(nil, "db_T")
	require.Len(t, infos, 1)
	require.Equal(t, s.Path(), infos[0].Path)
	require.Equal(t, fileSize(t, s.Path()), infos[0].Size)
}
