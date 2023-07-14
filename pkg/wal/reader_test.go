package wal_test

import (
	"context"
	"io"
	"testing"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

func TestSegmentReader(t *testing.T) {
	dir := t.TempDir()
	s, err := wal.NewSegment(dir, "Foo")
	require.NoError(t, err)
	require.NoError(t, s.Write(context.Background(), []byte("test")))
	require.NoError(t, s.Write(context.Background(), []byte("test1")))
	require.NoError(t, s.Write(context.Background(), []byte("test2")))
	require.NoError(t, s.Close())

	s, err = wal.Open(s.Path())
	require.NoError(t, err)

	r, err := s.Reader()
	require.NoError(t, err)
	b, err := io.ReadAll(r)
	require.NoError(t, err)

	require.Equal(t, "testtest1test2", string(b))
}
