package otlp

import (
	"context"
	"fmt"
	"testing"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/metrics/v1"
	commonv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	metricsv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/metrics/v1"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/remote"
	"github.com/Azure/adx-mon/transform"
)

type discardClient struct{}

func (discardClient) CloseIdleConnections()                             {}
func (discardClient) Write(context.Context, *prompb.WriteRequest) error { return nil }

func benchRequest(points int) *v1.ExportMetricsServiceRequest {
	dataPoints := make([]*metricsv1.NumberDataPoint, 0, points)
	for n := 0; n < points; n++ {
		dataPoints = append(dataPoints, &metricsv1.NumberDataPoint{
			TimeUnixNano: uint64(n) * 1e6,
			Value:        &metricsv1.NumberDataPoint_AsDouble{AsDouble: float64(n)},
			Attributes: []*commonv1.KeyValue{{
				Key:   "pod",
				Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: fmt.Sprintf("pod-%d", n%16)}},
			}},
		})
	}

	return &v1.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricsv1.ResourceMetrics{{
			ScopeMetrics: []*metricsv1.ScopeMetrics{{
				Metrics: []*metricsv1.Metric{{
					Name: "bench",
					Data: &metricsv1.Metric_Gauge{Gauge: &metricsv1.Gauge{DataPoints: dataPoints}},
				}},
			}},
		}},
	}
}

// BenchmarkOltpMetricWriterWrite covers the common path where nothing is shed,
// which must not pay for per-data-point rejection tracking.
func BenchmarkOltpMetricWriterWrite(b *testing.B) {
	w := NewOltpMetricWriter(OltpMetricWriterOpts{
		RequestTransformer: &transform.RequestTransformer{},
		MaxBatchSize:       500,
		Clients:            []remote.RemoteWriteClient{discardClient{}},
		HealthChecker:      metricHealthChecker{},
	})
	req := benchRequest(2000)

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if err := w.Write(context.Background(), req); err != nil {
			b.Fatal(err)
		}
	}
}
