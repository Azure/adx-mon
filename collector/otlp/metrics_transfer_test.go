package otlp

import (
	"bytes"
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/metrics/v1/metricsv1connect"
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/metrics/v1"
	metricsv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/metrics/v1"
	"github.com/bufbuild/connect-go"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/proto"
)

func TestMetricsService_OLTP_protobuf(t *testing.T) {
	type testcase struct {
		name             string
		writer           MetricWriter
		input            []byte
		overriddenLength int64
		expectedStatus   int
	}

	testcases := []testcase{
		{
			name:           "valid request",
			writer:         metricWriterWithError(nil),
			input:          serializedMSR(t, newServiceRequest()),
			expectedStatus: http.StatusOK,
		},
		{
			name:             "too short content length",
			writer:           metricWriterWithError(nil),
			input:            serializedMSR(t, newServiceRequest()),
			overriddenLength: 1,
			expectedStatus:   http.StatusBadRequest,
		},
		{
			name:             "not enough bytes",
			writer:           metricWriterWithError(nil),
			input:            serializedMSR(t, newServiceRequest()),
			overriddenLength: 1024,
			expectedStatus:   http.StatusInternalServerError,
		},
		{
			name:           "writer got unknown metric",
			writer:         metricWriterWithError(ErrUnknownMetricType),
			input:          serializedMSR(t, newServiceRequest()),
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:   "writer got rejected error",
			writer: metricWriterWithError(&ErrRejectedMetric{Msg: "do not support this metric", Count: 10}),
			input:  serializedMSR(t, newServiceRequest()),
			// per spec, must still accept the message.
			expectedStatus: http.StatusOK,
		},
		{
			name:           "writer had write error",
			writer:         metricWriterWithError(&ErrWriteError{Err: fs.ErrPermission}),
			input:          serializedMSR(t, newServiceRequest()),
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name:           "writer returned unknown error",
			writer:         metricWriterWithError(fs.ErrPermission),
			input:          serializedMSR(t, newServiceRequest()),
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMetricsService(tc.writer, "/fake/path", 0)
			resp := httptest.NewRecorder()

			req := httptest.NewRequest("POST", "/fake/path", bytes.NewReader(tc.input))
			req.Header.Set("Content-Type", "application/x-protobuf")
			if tc.overriddenLength > 0 {
				req.ContentLength = tc.overriddenLength
			}

			s.Handler(resp, req)
			require.Equal(t, tc.expectedStatus, resp.Code)
			// All responses must be application/x-protobuf, to match the request.
			require.Equal(t, "application/x-protobuf", resp.Header().Get("Content-Type"))

			// Per spec, we must return a Status object for errors or ExportMetricsServiceResponse for success.
			if resp.Code >= 400 {
				// If the response is an error, it must be a status.
				var status status.Status
				require.NoError(t, proto.Unmarshal(resp.Body.Bytes(), &status))
				require.NotNil(t, status.GetMessage())
			} else {
				// If the response is not an error, it must be a response.
				var response v1.ExportMetricsServiceResponse
				require.NoError(t, proto.Unmarshal(resp.Body.Bytes(), &response))
			}
		})
	}
}

func TestMetricsService_OLTP_GRPC(t *testing.T) {
	type testcase struct {
		name         string
		writer       MetricWriter
		input        *v1.ExportMetricsServiceRequest
		expectedCode connect.Code
	}

	testcases := []testcase{
		{
			name:   "valid request",
			writer: metricWriterWithError(nil),
			input:  newServiceRequest(),
		},
		{
			name:         "writer got unknown metric",
			writer:       metricWriterWithError(ErrUnknownMetricType),
			input:        newServiceRequest(),
			expectedCode: connect.CodeInvalidArgument,
		},
		{
			name:   "writer got rejected error",
			writer: metricWriterWithError(&ErrRejectedMetric{Msg: "do not support this metric", Count: 10}),
			input:  newServiceRequest(),
			// per spec, must still accept the message.
		},
		{
			name:         "writer had write error",
			writer:       metricWriterWithError(&ErrWriteError{Err: fs.ErrPermission}),
			input:        newServiceRequest(),
			expectedCode: connect.CodeUnavailable,
		},
		{
			name:         "writer returned unknown error",
			writer:       metricWriterWithError(fs.ErrPermission),
			input:        newServiceRequest(),
			expectedCode: connect.CodeUnavailable,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMetricsService(tc.writer, "/fake/path", 0)
			mux := http.NewServeMux()
			mux.Handle(metricsv1connect.NewMetricsServiceHandler(s))

			srv := httptest.NewUnstartedServer(mux)
			srv.EnableHTTP2 = true
			srv.StartTLS()
			defer srv.Close()

			client := metricsv1connect.NewMetricsServiceClient(srv.Client(), srv.URL, connect.WithGRPC())
			_, err := client.Export(context.Background(), connect.NewRequest(tc.input))
			if tc.expectedCode != 0 {
				require.Error(t, err)
				require.Equal(t, tc.expectedCode, connect.CodeOf(err))
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMetricsService_OLTP_ContentTypes(t *testing.T) {
	type testcase struct {
		contentType string
	}

	testcases := []testcase{
		{
			contentType: "application/json",
		},
		{
			contentType: "text/plain",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.contentType, func(t *testing.T) {
			s := NewMetricsService(metricWriterWithError(nil), "/fake/path", 0)
			resp := httptest.NewRecorder()

			req := httptest.NewRequest("POST", "/fake/path", bytes.NewReader(serializedMSR(t, newServiceRequest())))
			req.Header.Set("Content-Type", tc.contentType)

			s.Handler(resp, req)
			require.Equal(t, http.StatusUnsupportedMediaType, resp.Code)
		})
	}
}

type ErrMetricWriter struct {
	Err error
}

func (e *ErrMetricWriter) Write(ctx context.Context, msg *v1.ExportMetricsServiceRequest) error {
	return e.Err
}

func metricWriterWithError(err error) MetricWriter {
	return &ErrMetricWriter{Err: err}
}

func newServiceRequest() *v1.ExportMetricsServiceRequest {
	return &v1.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricsv1.ResourceMetrics{
			{
				ScopeMetrics: []*metricsv1.ScopeMetrics{
					{
						Metrics: []*metricsv1.Metric{
							{
								Name: "test",
								Data: &metricsv1.Metric_Gauge{
									Gauge: &metricsv1.Gauge{
										DataPoints: []*metricsv1.NumberDataPoint{
											{
												Value: &metricsv1.NumberDataPoint_AsInt{
													AsInt: 1,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func serializedMSR(t *testing.T, msg *v1.ExportMetricsServiceRequest) []byte {
	t.Helper()
	b, err := proto.Marshal(msg)
	require.NoError(t, err)
	return b
}
