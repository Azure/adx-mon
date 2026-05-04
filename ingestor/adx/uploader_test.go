package adx

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"testing"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/azure-kusto-go/azkustodata"
	kgzip "github.com/klauspost/compress/gzip"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

type failingReader struct {
	err error
}

func (f *failingReader) Read(_ []byte) (int, error) {
	return 0, f.err
}

func TestClusterRequiresDirectIngest(t *testing.T) {
	testutils.IntegrationTest(t)

	ctx := context.Background()
	k, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithStarted())
	testcontainers.CleanupContainer(t, k)
	require.NoError(t, err)

	cb := azkustodata.NewConnectionStringBuilder(k.ConnectionUrl())
	client, err := azkustodata.New(cb)
	require.NoError(t, err)
	defer client.Close()

	u := &uploader{
		KustoCli: client,
		database: "NetDefaultDB",
	}
	requiresDirectIngest, err := u.clusterRequiresDirectIngest(ctx)
	require.NoError(t, err)
	require.True(t, requiresDirectIngest)
}

func TestUploader_newGzipReader_CompressesInput(t *testing.T) {
	u := NewUploader(nil, UploaderOpts{ConcurrentUploads: 4})
	input := bytes.Repeat([]byte("foo,bar,baz\n"), 1024)

	gzipReader := u.newGzipReader(bytes.NewReader(input))
	defer gzipReader.Close()

	decoder, err := kgzip.NewReader(gzipReader)
	require.NoError(t, err)

	output, err := io.ReadAll(decoder)
	require.NoError(t, err)
	require.NoError(t, decoder.Close())
	require.Equal(t, input, output)
}

func TestUploader_newGzipReader_PropagatesSourceError(t *testing.T) {
	expectedErr := errors.New("source read failed")
	u := NewUploader(nil, UploaderOpts{ConcurrentUploads: 2})

	gzipReader := u.newGzipReader(&failingReader{err: expectedErr})
	defer gzipReader.Close()

	_, err := io.ReadAll(gzipReader)
	require.Error(t, err)
	require.ErrorIs(t, err, expectedErr)
}

func TestUploader_newGzipReader_VirtualRepairOnCorruptTailSegment(t *testing.T) {
	dir := t.TempDir()
	u := NewUploader(nil, UploaderOpts{ConcurrentUploads: 2})

	goodPath := writeSegment(t, dir, "Foo", []string{"good-a\n", "good-b\n"})
	corruptPath := writeSegment(t, dir, "Foo", []string{"good-c\n", "bad-tail\n"})
	trailingGoodPath := writeSegment(t, dir, "Foo", []string{"good-d\n", "good-e\n"})
	corruptLastBlockCRCInFile(t, corruptPath)

	goodReader, err := wal.NewSegmentReader(goodPath)
	require.NoError(t, err)
	defer goodReader.Close()

	corruptReader, err := wal.NewSegmentReader(corruptPath)
	require.NoError(t, err)
	defer corruptReader.Close()

	trailingGoodReader, err := wal.NewSegmentReader(trailingGoodPath)
	require.NoError(t, err)
	defer trailingGoodReader.Close()

	gzipReader := u.newGzipReader(io.MultiReader(goodReader, corruptReader, trailingGoodReader))
	defer gzipReader.Close()

	decoder, err := kgzip.NewReader(gzipReader)
	require.NoError(t, err)
	defer decoder.Close()

	output, err := io.ReadAll(decoder)
	require.NoError(t, err)
	require.Equal(t, "good-a\ngood-b\ngood-c\ngood-d\ngood-e\n", string(output))
}

func writeSegment(t *testing.T, dir, prefix string, rows []string) string {
	t.Helper()

	s, err := wal.NewSegment(dir, prefix)
	require.NoError(t, err)
	defer s.Close()

	for _, row := range rows {
		_, err = s.Write(context.Background(), []byte(row))
		require.NoError(t, err)
	}

	path := s.Path()
	require.NoError(t, s.Close())
	return path
}

func corruptLastBlockCRCInFile(t *testing.T, path string) {
	t.Helper()

	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	_, err = f.Seek(8, io.SeekStart)
	require.NoError(t, err)

	var header [8]byte
	for {
		headerPos, err := f.Seek(0, io.SeekCurrent)
		require.NoError(t, err)

		_, err = io.ReadFull(f, header[:])
		require.NoError(t, err)

		blockLen := binary.BigEndian.Uint32(header[:4])
		crcPos := headerPos + 4

		block := make([]byte, blockLen)
		_, err = io.ReadFull(f, block)
		require.NoError(t, err)

		nextHeaderPos, err := f.Seek(0, io.SeekCurrent)
		require.NoError(t, err)
		if _, err = io.ReadFull(f, header[:]); err == io.EOF {
			checksum := crc32.ChecksumIEEE(block)
			binary.BigEndian.PutUint32(header[:4], checksum+1)
			_, err = f.WriteAt(header[:4], crcPos)
			require.NoError(t, err)
			return
		}
		require.NoError(t, err)
		_, err = f.Seek(nextHeaderPos, io.SeekStart)
		require.NoError(t, err)
	}
}
