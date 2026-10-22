package wal_test

import (
	"context"
	"encoding/binary"
	"hash/crc32"
	"io"
	"os"
	"testing"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

func TestSegmentReader(t *testing.T) {
	tests := []struct {
		Name string
	}{
		{
			Name: "Disk",
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()
			s, err := wal.NewSegment(dir, "Foo")
			require.NoError(t, err)
			n, err := s.Write(context.Background(), []byte("test"))
			require.NoError(t, err)
			require.True(t, n > 0)
			n, err = s.Write(context.Background(), []byte("test1"))
			require.NoError(t, err)
			require.True(t, n > 0)
			n, err = s.Write(context.Background(), []byte("test2"))
			require.NoError(t, err)
			require.True(t, n > 0)
			require.NoError(t, s.Close())

			s, err = wal.Open(s.Path())
			require.NoError(t, err)

			r, err := s.Reader()
			require.NoError(t, err)
			b, err := io.ReadAll(r)
			require.NoError(t, err)

			require.Equal(t, "testtest1test2", string(b))
		})
	}
}

func TestSegmentReader_VirtualRepair_DropsCorruptTail(t *testing.T) {
	dir := t.TempDir()
	s, err := wal.NewSegment(dir, "Foo")
	require.NoError(t, err)

	_, err = s.Write(context.Background(), []byte("good1\n"))
	require.NoError(t, err)
	_, err = s.Write(context.Background(), []byte("good2\n"))
	require.NoError(t, err)
	_, err = s.Write(context.Background(), []byte("bad\n"))
	require.NoError(t, err)
	require.NoError(t, s.Close())

	corruptLastBlockCRC(t, s.Path())

	r, err := wal.NewSegmentReader(s.Path())
	require.NoError(t, err)

	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, "good1\ngood2\n", string(data))
	require.NoError(t, r.Close())

	r2, err := wal.NewSegmentReader(s.Path())
	require.NoError(t, err)

	data, err = io.ReadAll(r2)
	require.NoError(t, err)
	require.Equal(t, "good1\ngood2\n", string(data))
	require.NoError(t, r2.Close())

	iterFile, err := os.Open(s.Path())
	require.NoError(t, err)

	iter, err := wal.NewSegmentIterator(iterFile)
	require.NoError(t, err)
	defer func() { require.NoError(t, iter.Close()) }()

	n, err := iter.Verify()
	require.EqualError(t, err, "block checksum verification failed")
	require.Zero(t, n)
}

func TestSegmentReader_VirtualRepair_MatchesRepairDiscardPoint(t *testing.T) {
	dir := t.TempDir()
	s, err := wal.NewSegment(dir, "Foo")
	require.NoError(t, err)

	_, err = s.Write(context.Background(), []byte("good1\n"))
	require.NoError(t, err)
	_, err = s.Write(context.Background(), []byte("bad\n"))
	require.NoError(t, err)
	_, err = s.Write(context.Background(), []byte("good-after-bad\n"))
	require.NoError(t, err)
	require.NoError(t, s.Close())

	corruptBlockCRCByIndex(t, s.Path(), 1)

	r, err := wal.NewSegmentReader(s.Path())
	require.NoError(t, err)

	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, "good1\n", string(data))
	require.NoError(t, r.Close())

	seg, err := wal.Open(s.Path())
	require.NoError(t, err)
	require.NoError(t, seg.Close())

	repairedReader, err := wal.NewSegmentReader(s.Path())
	require.NoError(t, err)
	defer repairedReader.Close()

	repairedData, err := io.ReadAll(repairedReader)
	require.NoError(t, err)
	require.Equal(t, string(data), string(repairedData))
}

func corruptLastBlockCRC(t *testing.T, path string) {
	t.Helper()
	corruptBlockCRCByIndex(t, path, -1)
}

func corruptBlockCRCByIndex(t *testing.T, path string, blockIndex int) {
	t.Helper()

	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	_, err = f.Seek(8, io.SeekStart)
	require.NoError(t, err)

	var (
		header [8]byte
		blocks []int64
	)

	for {
		headerPos, err := f.Seek(0, io.SeekCurrent)
		require.NoError(t, err)

		_, err = io.ReadFull(f, header[:])
		require.NoError(t, err)

		blockLen := binary.BigEndian.Uint32(header[:4])
		blocks = append(blocks, headerPos+4)

		block := make([]byte, blockLen)
		_, err = io.ReadFull(f, block)
		require.NoError(t, err)

		nextHeaderPos, err := f.Seek(0, io.SeekCurrent)
		require.NoError(t, err)
		if _, err = io.ReadFull(f, header[:]); err == io.EOF {
			if blockIndex < 0 {
				blockIndex = len(blocks) - 1
			}
			require.GreaterOrEqual(t, blockIndex, 0)
			require.Less(t, blockIndex, len(blocks))

			blockPos := blocks[blockIndex] - 4
			_, err = f.ReadAt(header[:], blockPos)
			require.NoError(t, err)

			blockLen = binary.BigEndian.Uint32(header[:4])
			block = make([]byte, blockLen)
			_, err = f.ReadAt(block, blockPos+8)
			require.NoError(t, err)

			checksum := crc32.ChecksumIEEE(block)
			binary.BigEndian.PutUint32(header[:4], checksum+1)
			_, err = f.WriteAt(header[:4], blocks[blockIndex])
			require.NoError(t, err)
			return
		}
		require.NoError(t, err)
		_, err = f.Seek(nextHeaderPos, io.SeekStart)
		require.NoError(t, err)
	}
}
