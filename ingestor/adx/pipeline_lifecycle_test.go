package adx

import (
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/wal"
	adxschema "github.com/Azure/adx-mon/schema"
	azkustoingest "github.com/Azure/azure-kusto-go/azkustoingest"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

type delayedStreamer struct {
	streamIngester
}

func (s delayedStreamer) FromReader(ctx context.Context, reader io.Reader, opts ...azkustoingest.FileOption) (*azkustoingest.Result, error) {
	time.Sleep(2 * time.Second)
	return s.streamIngester.FromReader(ctx, reader, opts...)
}

func lifecycleHistogram(t *testing.T, step, route string) *dto.Histogram {
	t.Helper()
	var metric dto.Metric
	h := metrics.PipelineSegmentDurationSeconds.WithLabelValues("ingestor", step, "db", "Cpu", route)
	require.NoError(t, h.(prometheus.Metric).Write(&metric))
	return metric.GetHistogram()
}

func TestUploader_LifecycleRoutes(t *testing.T) {
	for _, tt := range []struct {
		name     string
		priority ingestpolicy.Priority
		route    string
		retry    bool
		fallback bool
	}{
		{name: "queued", route: "adx_queued"},
		{name: "queued retry", route: "adx_queued", retry: true},
		{name: "streamed", priority: ingestpolicy.PriorityRealtime, route: "adx_streaming"},
		{name: "streaming retry", priority: ingestpolicy.PriorityRealtime, route: "adx_streaming", retry: true},
		{name: "streaming fallback", priority: ingestpolicy.PriorityRealtime, route: "adx_queued", fallback: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			metrics.PipelineSegmentDurationSeconds.Reset()
			metrics.PipelineSegmentRetriesTotal.Reset()
			metrics.PipelineStepDurationSeconds.Reset()
			metrics.PipelineRetriesScheduledTotal.Reset()
			t.Cleanup(metrics.PipelineSegmentDurationSeconds.Reset)
			t.Cleanup(metrics.PipelineSegmentRetriesTotal.Reset)
			t.Cleanup(metrics.PipelineStepDurationSeconds.Reset)
			t.Cleanup(metrics.PipelineRetriesScheduledTotal.Reset)
			env := newStreamingTestEnv(t)
			env.addSegment(t, "Cpu", tt.priority, testCSV, time.Time{})
			synctest.Test(t, func(t *testing.T) {
				now := time.Now()
				infos := env.idx.Get(nil, "db_Cpu")
				require.Len(t, infos, 1)
				info := infos[0]
				env.idx.Remove(info)
				info.CreatedAt = now.Add(-3 * time.Second)
				info.Timing = wal.NewSegmentTiming(info.CreatedAt, now, false)
				env.idx.Add(info)
				env.u.streamer = delayedStreamer{env.stream}
				if tt.retry {
					env.stream.err = newStreamingHTTPError("500 Internal Server Error", 500, "")
				}
				if tt.fallback {
					env.stream.err = newStreamingHTTPError("413 Request Entity Too Large", 413, "")
				}
				calls := 0
				env.u.queuedUpload = func(_ context.Context, reader io.Reader, _, _ string, _ adxschema.SchemaMapping) error {
					_, err := io.Copy(io.Discard, reader)
					require.NoError(t, err)
					calls++
					time.Sleep(2 * time.Second)
					if tt.retry && calls == 1 {
						return errors.New("retry queued upload")
					}
					return nil
				}
				time.Sleep(2 * time.Second)
				batch := env.nextBatch(t, tt.priority)
				time.Sleep(4 * time.Second)
				env.u.uploadBatch(context.Background(), batch)
				if tt.retry {
					require.Zero(t, lifecycleHistogram(t, "total", tt.route).GetSampleCount())
					env.stream.err = nil
					time.Sleep(5 * time.Second)
					batch = env.nextBatch(t, tt.priority)
					time.Sleep(time.Second)
					env.u.uploadBatch(context.Background(), batch)
				}
				require.True(t, batch.IsRemoved())
				require.NoFileExists(t, info.Path)
				// A repeated success/cleanup must not double-count the segment.
				batch.RecordCompletion("ingestor", tt.route, batch.Segments)
				want := map[string]float64{
					"wal_buffer": 3, "batch_wait": 2, "retry_wait": 0, "worker_queue": 4, "processing": 2,
				}
				if tt.retry {
					want["retry_wait"], want["worker_queue"], want["processing"] = 5, 5, 4
				}
				if tt.fallback {
					want["processing"] = 4
				}
				var sum float64
				for step, seconds := range want {
					h := lifecycleHistogram(t, step, tt.route)
					require.EqualValues(t, 1, h.GetSampleCount(), step)
					require.Equal(t, seconds, h.GetSampleSum(), step)
					sum += seconds
				}
				total := lifecycleHistogram(t, "total", tt.route)
				require.EqualValues(t, 1, total.GetSampleCount())
				require.Equal(t, sum, total.GetSampleSum())
				complete := lifecycleHistogram(t, "complete_total", tt.route)
				require.Equal(t, total.GetSampleCount(), complete.GetSampleCount())
				require.Equal(t, sum, complete.GetSampleSum())
				retries := counterValue(t, metrics.PipelineSegmentRetriesTotal.WithLabelValues("ingestor", "db", "Cpu", tt.route))
				if tt.retry {
					require.Equal(t, float64(1), retries)
				} else {
					require.Zero(t, retries)
				}
			})
		})
	}
}

func TestStreamingRetryMetrics_Context(t *testing.T) {
	for _, tt := range []struct {
		name    string
		err     error
		retries float64
	}{
		{name: "slot unavailable", retries: 1},
		{name: "cancellation", err: context.Canceled},
		{name: "timeout", err: context.DeadlineExceeded, retries: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			metrics.PipelineRetriesScheduledTotal.Reset()
			t.Cleanup(metrics.PipelineRetriesScheduledTotal.Reset)
			ctx := context.Background()
			if tt.err == context.Canceled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			} else if tt.err == context.DeadlineExceeded {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
			}
			env := newStreamingTestEnv(t)
			outcome, _ := env.u.retry(ctx, "Cpu", tt.name)
			require.Equal(t, streamRetry, outcome)
			require.Equal(t, tt.retries, counterValue(t,
				metrics.PipelineRetriesScheduledTotal.WithLabelValues("ingestor", "adx_streaming_upload", "db", "Cpu")))
		})
	}
}
