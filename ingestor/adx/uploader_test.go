package adx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
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
