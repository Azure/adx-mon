package cluster

import (
	"testing"
	"time"

	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestBatch_LifecycleIncompleteHistory(t *testing.T) {
	for _, reason := range []string{"recovered", "overlapping_attempts", "missing_enqueue"} {
		t.Run(reason, func(t *testing.T) {
			metrics.PipelineSegmentDurationSeconds.Reset()
			metrics.PipelineIncompleteSegmentsTotal.Reset()
			t.Cleanup(metrics.PipelineSegmentDurationSeconds.Reset)
			t.Cleanup(metrics.PipelineIncompleteSegmentsTotal.Reset)
			now := time.Now()
			timing := wal.NewSegmentTiming(now.Add(-time.Second), now, reason == "recovered")
			batch := &Batch{Database: "db", Table: "Cpu", Segments: []wal.SegmentInfo{{Timing: timing}}, queuedAt: now}
			if reason == "missing_enqueue" {
				batch.queuedAt = time.Time{}
			}
			batch.StartProcessing()
			if reason == "overlapping_attempts" {
				batch.StartProcessing()
			}
			batch.RecordCompletion("ingestor", "adx_queued", batch.Segments)
			batch.RecordCompletion("ingestor", "adx_queued", batch.Segments)
			var metric dto.Metric
			for _, step := range []string{"total", "wal_buffer", "processing"} {
				h := metrics.PipelineSegmentDurationSeconds.WithLabelValues("ingestor", step, "db", "Cpu", "adx_queued")
				require.NoError(t, h.(prometheus.Metric).Write(&metric))
				if step == "total" {
					require.EqualValues(t, 1, metric.GetHistogram().GetSampleCount())
					require.GreaterOrEqual(t, metric.GetHistogram().GetSampleSum(), float64(1))
				} else {
					require.Zero(t, metric.GetHistogram().GetSampleCount())
				}
			}
			c := metrics.PipelineIncompleteSegmentsTotal.WithLabelValues("ingestor", "db", "Cpu", "adx_queued", reason)
			require.NoError(t, c.Write(&metric))
			require.Equal(t, float64(1), metric.GetCounter().GetValue())
		})
	}
}
