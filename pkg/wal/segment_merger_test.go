package wal

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewMultiReader_NotExists(t *testing.T) {
	mr, err := NewSegmentMerger("testdata/1", "testdata/2")
	require.True(t, os.IsNotExist(err))
	require.Nil(t, mr)
}

func TestNewMultiReader(t *testing.T) {
	dir := t.TempDir()
	s1, err := NewSegment(dir, "Db_Table")
	require.NoError(t, err)
	defer s1.Close()
	n, err := s1.Write(context.Background(), []byte("foo"))
	require.NoError(t, err)
	require.True(t, n > 0)
	n, err = s1.Write(context.Background(), []byte("bar"))
	require.NoError(t, err)
	require.True(t, n > 0)
	require.NoError(t, s1.Flush())

	s2, err := NewSegment(dir, "Db_Table")
	require.NoError(t, err)
	defer s2.Close()
	n, err = s2.Write(context.Background(), []byte("foo1"))
	require.NoError(t, err)
	require.True(t, n > 0)
	n, err = s2.Write(context.Background(), []byte("bar1"))
	require.NoError(t, err)
	require.True(t, n > 0)
	require.NoError(t, s2.Flush())

	mr, err := NewSegmentMerger(s1.Path(), s2.Path())
	require.NoError(t, err)
	require.NotNil(t, mr)

	b, err := io.ReadAll(mr)
	require.NoError(t, err)

	mergedPath := filepath.Join(dir, "Db_Table_123.wal")
	require.NoError(t, os.WriteFile(mergedPath, b, 0644))

	merged, err := NewSegmentReader(mergedPath)
	require.NoError(t, err)
	defer merged.Close()

	b, err = io.ReadAll(merged)
	require.NoError(t, err)

	require.Equal(t, "foobarfoo1bar1", string(b))
}
