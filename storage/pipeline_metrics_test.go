package storage_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/storage"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestStore_ImportPipelineMetrics(t *testing.T) {
	seg, err := wal.NewSegment(t.TempDir(), "db_Receive")
	require.NoError(t, err)
	_, err = seg.Write(context.Background(), []byte("data\n"))
	require.NoError(t, err)
	require.NoError(t, seg.Close())
	data, err := os.ReadFile(seg.Path())
	require.NoError(t, err)
	for _, tt := range []struct {
		name   string
		body   func() io.ReadCloser
		result string
		wrote  bool
	}{
		{name: "success", body: func() io.ReadCloser { return io.NopCloser(bytes.NewReader(data)) }, result: "success", wrote: true},
		{name: "read error", body: func() io.ReadCloser { return io.NopCloser(shortReader{}) }, result: "error"},
		{name: "append error", body: func() io.ReadCloser { return io.NopCloser(bytes.NewReader([]byte("invalid"))) }, result: "error", wrote: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			metrics.PipelineStepDurationSeconds.Reset()
			t.Cleanup(metrics.PipelineStepDurationSeconds.Reset)
			store := storage.NewLocalStore(storage.StoreOpts{
				StorageDir: t.TempDir(), Component: "ingestor", SegmentMaxAge: time.Hour, MaxDiskUsage: 1024 * 1024,
			})
			require.NoError(t, store.Open(context.Background()))
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			body := tt.body()
			defer body.Close()
			n, err := store.Import("db_Receive_123.wal", body)
			if tt.result == "error" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, len(data), n)
			}
			for _, step := range []string{"receive_transfer", "wal_write"} {
				var metric dto.Metric
				h := metrics.PipelineStepDurationSeconds.WithLabelValues("ingestor", step, "db", "Receive", tt.result)
				require.NoError(t, h.(prometheus.Metric).Write(&metric))
				if step == "wal_write" && !tt.wrote {
					require.Zero(t, metric.GetHistogram().GetSampleCount())
				} else {
					require.EqualValues(t, 1, metric.GetHistogram().GetSampleCount())
				}
			}
		})
	}
}
