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
			require.NoError(t, s.Write(context.Background(), []byte("test")))
			require.NoError(t, s.Write(context.Background(), []byte("test1")))
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
