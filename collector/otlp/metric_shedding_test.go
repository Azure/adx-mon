package otlp

import (
	metricsv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/metrics/v1"
	"context"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/remote"
	"github.com/Azure/adx-mon/storage"
	"github.com/Azure/adx-mon/transform"
	"github.com/stretchr/testify/require"
	"testing"
)

type sheddingClient struct{ calls int }

func (c *sheddingClient) CloseIdleConnections() {}
func (c *sheddingClient) Write(_ context.Context, r *prompb.WriteRequest) error {
	c.calls++
	return &storage.WriteOutcome{Rejected: int64(len(r.Timeseries)), RejectedSeries: append([]*prompb.TimeSeries(nil), r.Timeseries...)}
}

func TestOTLPSheddingCountsOriginalHistogramPointsAcrossFlushes(t *testing.T) {
	c := &sheddingClient{}
	w := NewOltpMetricWriter(OltpMetricWriterOpts{RequestTransformer: &transform.RequestTransformer{}, MaxBatchSize: 1, Clients: []remote.RemoteWriteClient{c}, HealthChecker: metricHealthChecker{}})
	r := newServiceRequest()
	r.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].Data = &metricsv1.Metric_Histogram{Histogram: &metricsv1.Histogram{DataPoints: []*metricsv1.HistogramDataPoint{{Count: 3, BucketCounts: []uint64{1, 2}, ExplicitBounds: []float64{1}}, {Count: 1}}}}
	var rejected *ErrRejectedMetric
	require.ErrorAs(t, w.Write(context.Background(), r), &rejected)
	require.EqualValues(t, 2, rejected.Count)
	require.Equal(t, 4, c.calls)
}
