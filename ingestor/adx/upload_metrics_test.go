package adx

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func histogram(t *testing.T, o prometheus.Observer) (count uint64, sum float64) {
	t.Helper()
	m := &dto.Metric{}
	require.NoError(t, o.(prometheus.Metric).Write(m))
	return m.GetHistogram().GetSampleCount(), m.GetHistogram().GetSampleSum()
}

// Upload latency is recorded for every batch so its distribution, and so its P99, can be measured.  The existing
// SampleLatency gauge only keeps each table's most recent value.
func TestUploadBatchRecordsLatency(t *testing.T) {
	env := newStreamingTestEnv(t)
	queued := metrics.IngestorUploadLatency.WithLabelValues("db", "queued")
	streaming := metrics.IngestorUploadLatency.WithLabelValues("db", "streaming")
	queuedCount, queuedSum := histogram(t, queued)
	streamingCount, streamingSum := histogram(t, streaming)

	// A batch that waited 100s, then one for the same table that waited 1s.
	env.addSegment(t, "Cpu", ingestpolicy.PriorityQueued, testCSV, time.Now().Add(-100*time.Second))
	env.u.uploadBatch(context.Background(), env.nextBatch(t, ingestpolicy.PriorityQueued))
	env.addSegment(t, "Cpu", ingestpolicy.PriorityQueued, testCSV, time.Now().Add(-time.Second))
	env.u.uploadBatch(context.Background(), env.nextBatch(t, ingestpolicy.PriorityQueued))
	t.Logf("sample_latency gauge after both batches: %.0fs", gaugeValue(t, metrics.SampleLatency.WithLabelValues("db", "Cpu")))

	count, sum := histogram(t, queued)
	t.Logf("upload_latency_seconds after both batches: %d samples totalling %.0fs", count-queuedCount, sum-queuedSum)
	require.EqualValues(t, 2, count-queuedCount)
	require.InDelta(t, 101, sum-queuedSum, 1)

	env.addSegment(t, "Cpu", ingestpolicy.PriorityRealtime, testCSV, time.Now().Add(-time.Second))
	env.u.uploadBatch(context.Background(), env.nextBatch(t, ingestpolicy.PriorityRealtime))
	count, sum = histogram(t, streaming)
	require.EqualValues(t, 1, count-streamingCount)
	require.InDelta(t, 1, sum-streamingSum, 1)

	count, _ = histogram(t, metrics.IngestorUploadDuration.WithLabelValues("db", "queued"))
	require.NotZero(t, count)
	count, _ = histogram(t, metrics.IngestorUploadDuration.WithLabelValues("db", "streaming"))
	require.NotZero(t, count)
}

// A failed queued upload is counted, and is not recorded as delivered.
func TestUploadBatchCountsFailures(t *testing.T) {
	env := newStreamingTestEnv(t)
	env.queue.err = errors.New("blob upload failed")
	failures := metrics.IngestorUploadFailures.WithLabelValues("db", "Failing")
	latency := metrics.IngestorUploadLatency.WithLabelValues("db", "queued")
	before := counterValue(t, failures)
	latencyCount, _ := histogram(t, latency)

	env.addSegment(t, "Failing", ingestpolicy.PriorityQueued, testCSV, time.Time{})
	env.u.uploadBatch(context.Background(), env.nextBatch(t, ingestpolicy.PriorityQueued))

	require.Equal(t, 1.0, counterValue(t, failures)-before)
	count, _ := histogram(t, latency)
	require.Equal(t, latencyCount, count)
}
