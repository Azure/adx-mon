package adx

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/Azure/adx-mon/pkg/wal"
	adxschema "github.com/Azure/adx-mon/schema"
	"github.com/Azure/azure-kusto-go/azkustodata"
	kgzip "github.com/klauspost/compress/gzip"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestUploader_PipelineMetricsAcrossRetry(t *testing.T) {
	metrics.PipelineStepDurationSeconds.Reset()
	metrics.PipelineRetriesScheduledTotal.Reset()
	t.Cleanup(metrics.PipelineStepDurationSeconds.Reset)
	t.Cleanup(metrics.PipelineRetriesScheduledTotal.Reset)
	env := newStreamingTestEnv(t)
	path := env.addSegment(t, "Cpu", ingestpolicy.PriorityQueued, testCSV, time.Time{})
	env.queue.err = errors.New("submission failed")
	first := env.nextBatch(t, ingestpolicy.PriorityQueued)

	env.u.uploadBatch(context.Background(), first)

	require.True(t, first.IsReleased())
	require.False(t, first.IsRemoved())
	require.FileExists(t, path)
	require.Equal(t, float64(1), pipelineRetriesScheduled(t))

	env.queue.err = nil
	second := env.nextBatch(t, ingestpolicy.PriorityQueued)
	env.u.uploadBatch(context.Background(), second)

	require.True(t, second.IsReleased())
	require.True(t, second.IsRemoved())
	require.NoFileExists(t, path)
	require.Equal(t, 2, env.queue.calls)
	require.Equal(t, testCSV, env.queue.data)
	require.Equal(t, float64(1), pipelineRetriesScheduled(t))
	for _, result := range []string{"success", "error"} {
		var m dto.Metric
		h := metrics.PipelineStepDurationSeconds.WithLabelValues("ingestor", "adx_queued_upload", result)
		require.NoError(t, h.(prometheus.Metric).Write(&m))
		require.EqualValues(t, 1, m.GetHistogram().GetSampleCount())
		require.Positive(t, m.GetHistogram().GetSampleSum())
	}
}

func TestUploader_PipelineMetricsCancellation(t *testing.T) {
	for _, duringUpload := range []bool{false, true} {
		name := "before upload"
		if duringUpload {
			name = "during upload"
		}
		t.Run(name, func(t *testing.T) {
			metrics.PipelineStepDurationSeconds.Reset()
			metrics.PipelineRetriesScheduledTotal.Reset()
			t.Cleanup(metrics.PipelineStepDurationSeconds.Reset)
			t.Cleanup(metrics.PipelineRetriesScheduledTotal.Reset)
			env := newStreamingTestEnv(t)
			env.addSegment(t, "Cpu", ingestpolicy.PriorityQueued, testCSV, time.Time{})
			batch := env.nextBatch(t, ingestpolicy.PriorityQueued)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			env.u.queuedUpload = func(ctx context.Context, _ io.Reader, _, _ string, _ adxschema.SchemaMapping) error {
				cancel()
				return ctx.Err()
			}
			if !duringUpload {
				cancel()
			}

			env.u.uploadBatch(ctx, batch)

			require.True(t, batch.IsReleased())
			require.False(t, batch.IsRemoved())
			require.Zero(t, pipelineRetriesScheduled(t))
			var m dto.Metric
			h := metrics.PipelineStepDurationSeconds.WithLabelValues("ingestor", "adx_queued_upload", "error")
			require.NoError(t, h.(prometheus.Metric).Write(&m))
			if duringUpload {
				require.EqualValues(t, 1, m.GetHistogram().GetSampleCount())
			} else {
				require.Zero(t, m.GetHistogram().GetSampleCount())
			}
		})
	}
}

func TestUploader_PipelineMetricsTimeout(t *testing.T) {
	metrics.PipelineStepDurationSeconds.Reset()
	metrics.PipelineRetriesScheduledTotal.Reset()
	t.Cleanup(metrics.PipelineStepDurationSeconds.Reset)
	t.Cleanup(metrics.PipelineRetriesScheduledTotal.Reset)
	env := newStreamingTestEnv(t)
	env.addSegment(t, "Cpu", ingestpolicy.PriorityQueued, testCSV, time.Time{})
	batch := env.nextBatch(t, ingestpolicy.PriorityQueued)

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		env.u.queuedUpload = func(ctx context.Context, _ io.Reader, _, _ string, _ adxschema.SchemaMapping) error {
			<-ctx.Done()
			return ctx.Err()
		}

		env.u.uploadBatch(ctx, batch)

		require.True(t, batch.IsReleased())
		require.False(t, batch.IsRemoved())
		require.Equal(t, float64(1), pipelineRetriesScheduled(t))
		var m dto.Metric
		h := metrics.PipelineStepDurationSeconds.WithLabelValues("ingestor", "adx_queued_upload", "error")
		require.NoError(t, h.(prometheus.Metric).Write(&m))
		require.EqualValues(t, 1, m.GetHistogram().GetSampleCount())
		require.Equal(t, float64(5), m.GetHistogram().GetSampleSum())
	})
}

func pipelineRetriesScheduled(t *testing.T) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, metrics.PipelineRetriesScheduledTotal.WithLabelValues("ingestor", "adx_queued_upload").Write(&m))
	return m.GetCounter().GetValue()
}

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

	var wg sync.WaitGroup
	gzipReader := u.newGzipReader(bytes.NewReader(input), &wg)
	defer wg.Wait()
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

	var wg sync.WaitGroup
	gzipReader := u.newGzipReader(&failingReader{err: expectedErr}, &wg)
	defer wg.Wait()
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

	var wg sync.WaitGroup
	gzipReader := u.newGzipReader(io.MultiReader(goodReader, corruptReader, trailingGoodReader), &wg)
	defer wg.Wait()
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
