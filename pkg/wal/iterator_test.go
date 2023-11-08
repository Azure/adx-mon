package wal_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/pkg/wal/file"
	"github.com/stretchr/testify/require"
)

func TestSegmentIterator_Verify(t *testing.T) {

	tests := []struct {
		Name            string
		StorageProvider file.Provider
	}{
		{Name: "Disk", StorageProvider: &file.DiskProvider{}},
		{Name: "Memory", StorageProvider: &file.MemoryProvider{}},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()

			iter, err := wal.NewSegmentIterator(io.NopCloser(bytes.NewBuffer([]byte("testtest1"))))
			require.NoError(t, err)
			defer iter.Close()

			require.Error(t, iter.Verify())

			s, err := wal.NewSegment(dir, "Foo", tt.StorageProvider)
			require.NoError(t, err)
			require.NoError(t, s.Write(context.Background(), []byte("test")))
			require.NoError(t, s.Write(context.Background(), []byte("test1")))
			require.NoError(t, s.Flush())

			f, err := tt.StorageProvider.Open(s.Path())
			require.NoError(t, err)
			iter, err = wal.NewSegmentIterator(f)
			require.NoError(t, err)
			defer iter.Close()
			defer f.Close()

			require.NoError(t, iter.Verify())

		})
	}
}
