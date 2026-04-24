package adx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/azure-kusto-go/azkustodata"
	kgzip "github.com/klauspost/compress/gzip"
	dto "github.com/prometheus/client_model/go"
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

func TestObserveUploadedHostLogs(t *testing.T) {
	metrics.IngestorLogsUploaded.Reset()

	dir := t.TempDir()
	hostReader := newSegmentReaderWithMetadata(t, dir, wal.HostLogSampleType, 2)
	defer hostReader.Close()
	otherReader := newSegmentReaderWithMetadata(t, dir, wal.LogSampleType, 5)
	defer otherReader.Close()

	observeUploadedHostLogs("Logs", "HostLogs", []*wal.SegmentReader{hostReader, otherReader})

	var m dto.Metric
	require.NoError(t, metrics.IngestorLogsUploaded.WithLabelValues("Logs", "HostLogs").Write(&m))
	require.Equal(t, float64(2), m.GetCounter().GetValue())
}

func newSegmentReaderWithMetadata(t *testing.T, dir string, sampleType wal.SampleType, count uint32) *wal.SegmentReader {
	t.Helper()

	seg, err := wal.NewSegment(dir, "Logs_HostLogs")
	require.NoError(t, err)

	_, err = seg.Write(context.Background(), []byte("value\n"), wal.WithSampleMetadata(sampleType, count))
	require.NoError(t, err)
	path := seg.Path()
	require.NoError(t, seg.Close())

	reader, err := wal.NewSegmentReader(path)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, reader)
	require.NoError(t, err)
	return reader
}
