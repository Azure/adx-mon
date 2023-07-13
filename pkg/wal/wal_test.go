package wal_test

import (
	"context"
	"testing"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

func TestNewWAL(t *testing.T) {
	w, err := wal.NewWAL(wal.WALOpts{
		StorageDir: t.TempDir(),
	})
	require.NoError(t, err)
	require.NoError(t, w.Open(context.Background()))

	w.Write(context.Background(), []byte("foo"))
	w.Write(context.Background(), []byte("foo"))
	w.Write(context.Background(), []byte("foo"))
	require.Equal(t, 1, w.Size())
}

func TestWAL_Segment(t *testing.T) {
	w, err := wal.NewWAL(wal.WALOpts{
		StorageDir: t.TempDir(),
	})
	require.NoError(t, err)
	require.NoError(t, w.Open(context.Background()))

	w.Write(context.Background(), []byte("1970-01-01T00:00:00.001Z,-414304664621325809,{},1.000000000\n"))
	w.Write(context.Background(), []byte("1970-01-01T00:00:00.002Z,-414304664621325809,{},2.000000000\n"))

	require.Equal(t, 1, w.Size())

	path := w.Path()

	require.NoError(t, w.Close())

	seg, err := wal.Open(path)
	require.NoError(t, err)

	b, err := seg.Bytes()
	require.NoError(t, err)
	require.Equal(t, `1970-01-01T00:00:00.001Z,-414304664621325809,{},1.000000000
1970-01-01T00:00:00.002Z,-414304664621325809,{},2.000000000
`, string(b))

}

func TestWAL_Open(t *testing.T) {
	dir := t.TempDir()
	w, err := wal.NewWAL(wal.WALOpts{
		Prefix:     "Foo",
		StorageDir: dir,
	})
	require.NoError(t, err)
	require.NoError(t, w.Open(context.Background()))
	w.Write(context.Background(), []byte("foo"))
	require.Equal(t, 1, w.Size())

	require.NoError(t, w.Close())

	w, err = wal.NewWAL(wal.WALOpts{
		Prefix:     "Foo",
		StorageDir: dir,
	})
	require.NoError(t, err)
	require.NoError(t, w.Open(context.Background()))
	require.Equal(t, 0, w.Size())

	w.Write(context.Background(), []byte("foo"))
	require.Equal(t, 1, w.Size())

}
