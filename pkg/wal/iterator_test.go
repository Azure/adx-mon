package wal_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

func TestSegmentIterator_Verify(t *testing.T) {

	tests := []struct {
		Name string
	}{
		{Name: "Disk"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()

			iter, err := wal.NewSegmentIterator(io.NopCloser(bytes.NewBuffer([]byte("ADXWAL  testtest1"))))
			require.NoError(t, err)
			defer iter.Close()

			n, err := iter.Verify()
			require.Error(t, err)
			require.Equal(t, 0, n)

			s, err := wal.NewSegment(dir, "Foo")
			require.NoError(t, err)
			n, err = s.Write(context.Background(), []byte("test"))
			require.NoError(t, err)
			require.True(t, n > 0)
			n, err = s.Write(context.Background(), []byte("test1"))
			require.NoError(t, err)
			require.True(t, n > 0)
			require.NoError(t, s.Flush())

			f, err := os.Open(s.Path())
			require.NoError(t, err)
			iter, err = wal.NewSegmentIterator(f)
			require.NoError(t, err)
			defer iter.Close()
			defer f.Close()

			n, err = iter.Verify()
			require.NoError(t, err)
			require.Equal(t, 2, n)

		})
	}
}

func TestSegmentIterator_Verify_TrailingBytes(t *testing.T) {

	tests := []struct {
		Name string
	}{
		{Name: "Disk"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()

			iter, err := wal.NewSegmentIterator(io.NopCloser(bytes.NewBuffer([]byte("ADXWAL  testtest1"))))
			require.NoError(t, err)
			defer iter.Close()

			n, err := iter.Verify()
			require.Error(t, err)
			require.Equal(t, 0, n)

			s, err := wal.NewSegment(dir, "Foo")
			require.NoError(t, err)
			n, err = s.Write(context.Background(), []byte("test"))
			require.NoError(t, err)
			require.True(t, n > 0)
			n, err = s.Write(context.Background(), []byte("test1"))
			require.NoError(t, err)
			require.True(t, n > 0)
			require.NoError(t, s.Flush())

			f, err := os.OpenFile(s.Path(), os.O_RDWR, 0644)
			require.NoError(t, err)

			// Seek to the end of the file
			_, err = f.Seek(0, io.SeekEnd)
			require.NoError(t, err)

			// Append some trailing 0 bytes
			_, err = f.Write(make([]byte, 1))
			require.NoError(t, err)
			_, err = f.Seek(0, io.SeekStart)

			// We should be able to iterate over the two good blocks and stop when we hit
			// the trailing zeros.
			iter, err = wal.NewSegmentIterator(f)
			require.NoError(t, err)
			defer iter.Close()
			defer f.Close()

			next, err := iter.Next()
			require.NoError(t, err)
			require.True(t, next)

			next, err = iter.Next()
			require.NoError(t, err)
			require.True(t, next)

			next, err = iter.Next()
			require.NoError(t, err)
			require.False(t, next)

			_, err = f.Seek(0, io.SeekStart)
			require.NoError(t, err)

			// Create a new iterator and check that Verify returns succeeds as it does
			// have a bad block at the end that could be repaired, but is ignored.
			iter, err = wal.NewSegmentIterator(f)
			require.NoError(t, err)
			defer iter.Close()
			defer f.Close()
			n, err = iter.Verify()
			require.NoError(t, err)
			require.Equal(t, 2, n)

		})
	}
}
