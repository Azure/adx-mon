package export

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/metrics/v1"
	commonv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	metricsv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/metrics/v1"
	"github.com/Azure/adx-mon/pkg/pool"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/transform"
	"github.com/golang/protobuf/proto"
	"github.com/stretchr/testify/require"
)

func BenchmarkWrite(b *testing.B) {
	// Set up the httptest server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	opts := PromToOtlpExporterOpts{
		Transformer: &transform.RequestTransformer{}, //TODO
		Destination: server.URL,
	}
	// Create the PromToOtlpForwarder
	exporter := NewPromToOtlpExporter(opts)

	req := &prompb.WriteRequest{
		Timeseries: []*prompb.TimeSeries{
			{
				Labels: []*prompb.Label{
					{
						Name:  []byte("__name__"),
						Value: []byte("cpu"),
					},
					{
						Name:  []byte("region"),
						Value: []byte("eastus"),
					},
					{
						Name:  []byte("adxmon_database"),
						Value: []byte("Metrics"),
					},
				},
				Samples: []*prompb.Sample{
					{
						Value:     1.0,
						Timestamp: 123456789,
					},
					{
						Value:     2.0,
						Timestamp: 123456789,
					},
				},
			},
			{
				Labels: []*prompb.Label{
					{
						Name:  []byte("__name__"),
						Value: []byte("mem"),
					},
					{
						Name:  []byte("region"),
						Value: []byte("westus"),
					},
					{
						Name:  []byte("adxmon_database"),
						Value: []byte("Metrics"),
					},
				},
				Samples: []*prompb.Sample{
					{
						Value:     1.0,
						Timestamp: 123456789,
					},
					{
						Value:     2.0,
						Timestamp: 123456789,
					},
				},
			},
		},
	}

	ctx := context.Background()

	// Run the benchmark
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := exporter.Write(ctx, req)
		if err != nil {
			b.Fatalf("Failed to forward metrics: %v", err)
		}
	}
}

func TestPromToOtlpRequest(t *testing.T) {
	type testcase struct {
		name     string
		exporter *PromToOtlpExporter
	}

	opts := PromToOtlpExporterOpts{
		Transformer: &transform.RequestTransformer{},
		Destination: "",
		AddResourceAttributes: map[string]string{
			"resourceAttributeOne": "resourceAttributeValueOne",
		},
	}

	testcases := []testcase{
		{
			name:     "default exporter",
			exporter: NewPromToOtlpExporter(opts),
		},
		{
			name:     "corrupted pool exporter",
			exporter: setupCorruptedPools(NewPromToOtlpExporter(opts)),
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			basets := int64(123456789)
			req := &prompb.WriteRequest{
				Timeseries: []*prompb.TimeSeries{
					{
						Labels: []*prompb.Label{
							{
								Name:  []byte("__name__"),
								Value: []byte("cpu"),
							},
							{
								Name:  []byte("region"),
								Value: []byte("eastus"),
							},
							{
								Name:  []byte("adxmon_database"),
								Value: []byte("Metrics"),
							},
						},
						Samples: []*prompb.Sample{
							{
								Value:     1.0,
								Timestamp: basets,
							},
							{
								Value:     2.0,
								Timestamp: basets + 20,
							},
						},
					},
					{
						Labels: []*prompb.Label{
							{
								Name:  []byte("__name__"),
								Value: []byte("mem"),
							},
							{
								Name:  []byte("region"),
								Value: []byte("westus"),
							},
							{
								Name:  []byte("type"),
								Value: []byte("used"),
							},
							{
								Name:  []byte("adxmon_database"),
								Value: []byte("Metrics2"),
							},
						},
						Samples: []*prompb.Sample{
							{
								Value:     3.0,
								Timestamp: basets + 40,
							},
							{
								Value:     4.0,
								Timestamp: basets + 60,
							},
						},
					},
				},
			}

			serialized, err := tc.exporter.promToOtlpRequest(req)
			require.NoError(t, err)
			require.NotEmpty(t, serialized)

			exportRequest := &v1.ExportMetricsServiceRequest{}
			err = proto.Unmarshal(serialized, exportRequest)
			require.NoError(t, err)

			// check resourcemetrics
			require.Len(t, exportRequest.ResourceMetrics, 1)
			resourceMetric := exportRequest.ResourceMetrics[0]
			require.Len(t, resourceMetric.Resource.Attributes, 1) // one resource attribute configured
			attribute := resourceMetric.Resource.Attributes[0]
			require.Equal(t, "resourceAttributeOne", attribute.Key)
			require.Equal(t, "resourceAttributeValueOne", attribute.Value.GetStringValue())

			// All metrics under one scope
			require.Len(t, resourceMetric.ScopeMetrics, 1)
			metrics := resourceMetric.ScopeMetrics[0].Metrics

			// two metric names
			require.Len(t, metrics, 2)

			cpuMetric := metrics[0]
			require.Equal(t, "cpu", cpuMetric.Name)
			// two gauge datapoints
			datapoints := cpuMetric.GetGauge().GetDataPoints()
			require.Len(t, datapoints, 2)
			require.Equal(t, datapoints[0].TimeUnixNano, toNano(basets))
			require.Equal(t, datapoints[0].GetAsDouble(), 1.0)
			validateAttributes(t, datapoints[0], map[string]string{"region": "eastus"})
			require.Equal(t, datapoints[1].TimeUnixNano, toNano(basets+20))
			require.Equal(t, datapoints[1].GetAsDouble(), 2.0)
			validateAttributes(t, datapoints[1], map[string]string{"region": "eastus"})

			memMetric := metrics[1]
			require.Equal(t, "mem", memMetric.Name)
			datapoints = memMetric.GetGauge().GetDataPoints()
			require.Len(t, datapoints, 2)
			require.Equal(t, datapoints[0].TimeUnixNano, toNano(basets+40))
			require.Equal(t, datapoints[0].GetAsDouble(), 3.0)
			validateAttributes(t, datapoints[0], map[string]string{"region": "westus", "type": "used"})
			require.Equal(t, datapoints[1].TimeUnixNano, toNano(basets+60))
			require.Equal(t, datapoints[1].GetAsDouble(), 4.0)
			validateAttributes(t, datapoints[1], map[string]string{"region": "westus", "type": "used"})
		})
	}
}

func TestSendRequest(t *testing.T) {
	type testcase struct {
		name           string
		exporter       *PromToOtlpExporter
		expectedStatus int
		response       *v1.ExportMetricsServiceResponse
	}

	opts := PromToOtlpExporterOpts{
		Transformer: &transform.RequestTransformer{},
		Destination: "",
	}

	testcases := []testcase{
		{
			name:           "successful request, full success",
			exporter:       NewPromToOtlpExporter(opts),
			expectedStatus: http.StatusOK,
			response:       &v1.ExportMetricsServiceResponse{},
		},
		{
			name:           "successful request, partial success",
			exporter:       NewPromToOtlpExporter(opts),
			expectedStatus: http.StatusOK,
			response: &v1.ExportMetricsServiceResponse{
				PartialSuccess: &v1.ExportMetricsPartialSuccess{
					RejectedDataPoints: 1,
				},
			},
		},
		{
			name:           "server error",
			exporter:       NewPromToOtlpExporter(opts),
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name:           "client error",
			exporter:       NewPromToOtlpExporter(opts),
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Helper()
				require.Equal(t, "POST", r.Method)
				require.Equal(t, "/v1/metrics", r.URL.Path)
				require.Equal(t, "application/x-protobuf", r.Header.Get("Content-Type"))

				if tc.response != nil {
					w.Header().Set("Content-Type", "application/x-protobuf")
					w.WriteHeader(tc.expectedStatus)
					serialized, err := proto.Marshal(tc.response)
					require.NoError(t, err)
					w.Write(serialized)
				} else {
					w.WriteHeader(tc.expectedStatus)
				}
			}))
			defer server.Close()

			tc.exporter.destination = server.URL + "/v1/metrics"

			body := []byte("test body")
			err := tc.exporter.sendRequest(body, 20)

			if tc.expectedStatus == http.StatusOK {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

// setupCorruptedPools purposely makes noisy object pools to catch lack of resets
func setupCorruptedPools(exporter *PromToOtlpExporter) *PromToOtlpExporter {
	exporter.stringKVPool = pool.NewGeneric(4096, func(sz int) interface{} {
		return &commonv1.KeyValue{
			Key:   "corrupted",
			Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "corrupted"}},
		}
	})

	exporter.datapointPool = pool.NewGeneric(4096, func(sz int) interface{} {
		return &metricsv1.NumberDataPoint{
			Attributes:   []*commonv1.KeyValue{{Key: "corrupted", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_DoubleValue{DoubleValue: 123.456}}}},
			TimeUnixNano: 123123,
			Value:        &metricsv1.NumberDataPoint_AsDouble{AsDouble: 456.789},
		}
	})
	return exporter
}

func validateAttributes(t *testing.T, datapoint *metricsv1.NumberDataPoint, expectedAttrs map[string]string) {
	t.Helper()

	attributesMap := make(map[string]string)
	for _, attr := range datapoint.Attributes {
		attributesMap[attr.Key] = attr.Value.GetStringValue()
	}

	require.Equal(t, expectedAttrs, attributesMap)
}

func toNano(ts int64) uint64 {
	return uint64(ts * 1000000)
}
