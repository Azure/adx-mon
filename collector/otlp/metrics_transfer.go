package otlp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/metrics/v1"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	connect "github.com/bufbuild/connect-go"
	gbp "github.com/libp2p/go-buffer-pool"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/proto"
)

type MetricsService struct {
	writer          MetricWriter
	logger          *slog.Logger
	httpReqReceived *prometheus.CounterVec
	grpcReqReceived *prometheus.CounterVec
}

func NewMetricsService(writer MetricWriter, path string, grpcPort int) *MetricsService {
	return &MetricsService{
		writer: writer,
		logger: slog.Default().With(
			slog.Group(
				"handler",
				slog.String("protocol", "otlp-metrics"),
			),
		),
		httpReqReceived: metrics.MetricsRequestsReceived.MustCurryWith(prometheus.Labels{"protocol": "oltp/http", "endpoint": path}),
		grpcReqReceived: metrics.MetricsRequestsReceived.MustCurryWith(prometheus.Labels{"protocol": "oltp/grpc", "endpoint": fmt.Sprintf(":%d", grpcPort)}),
	}
}

// Export implements metricsv1connect.MetricsServiceHandler to handle OLTP/GRPC metrics requests
func (s *MetricsService) Export(ctx context.Context, req *connect.Request[v1.ExportMetricsServiceRequest]) (*connect.Response[v1.ExportMetricsServiceResponse], error) {
	resp, status, code := s.writeMessage(ctx, req.Msg)
	if status != nil {
		var c connect.Code = connect.CodeUnavailable
		if code >= 400 && code < 500 {
			c = connect.CodeInvalidArgument
		}

		err := connect.NewError(c, nil)
		if detail, detailErr := connect.NewErrorDetail(status); detailErr == nil {
			err.AddDetail(detail)
		}
		s.grpcReqReceived.WithLabelValues(c.String()).Inc()

		return nil, err
	}

	s.grpcReqReceived.WithLabelValues("ok").Inc()
	return connect.NewResponse(resp), nil
}

// Handler handles OTLP/HTTP metrics requests
// See https://opentelemetry.io/docs/specs/otlp/#otlphttp
func (s *MetricsService) Handler(w http.ResponseWriter, r *http.Request) {
	m := s.httpReqReceived
	defer r.Body.Close()

	ctx := r.Context()
	switch r.Header.Get("Content-Type") {
	case "application/x-protobuf":

		// Consume the request body and marshal into a protobuf
		b := gbp.Get(int(r.ContentLength))
		defer gbp.Put(b)

		n, err := io.ReadFull(r.Body, b)
		if err != nil {
			s.logger.Error("Failed to read request body", "Error", err)
			status := newErrorStatus("Failed to read request body.")
			writeErrorStatusResponse(w, http.StatusInternalServerError, status, m)
			return
		}
		if n < int(r.ContentLength) {
			s.logger.Warn("Short read")
			status := newErrorStatus("Body did not contain enough bytes.")
			writeErrorStatusResponse(w, http.StatusBadRequest, status, m)
			return
		}
		if logger.IsDebug() {
			s.logger.Debug("Received request body", "Bytes", n)
		}

		msg := &v1.ExportMetricsServiceRequest{}
		if err := proto.Unmarshal(b, msg); err != nil {
			s.logger.Error("Failed to unmarshal request body", "Error", err)
			status := newErrorStatus("Unable to unmarshal ExportMetricsServiceRequest.")
			writeErrorStatusResponse(w, http.StatusBadRequest, status, m)
			return
		}

		resp, status, code := s.writeMessage(ctx, msg)
		if status != nil {
			writeErrorStatusResponse(w, code, status, m)
		} else {
			writeExportMetricsServiceResponse(w, resp, m)
		}
	default:
		logger.Errorf("Unsupported Content-Type: %s", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusUnsupportedMediaType)
		m.WithLabelValues(strconv.Itoa(http.StatusUnsupportedMediaType)).Inc()
	}
}

// writeMessage writes the metrics to the writer and returns the response, status message (for errors), and HTTP status code
func (s *MetricsService) writeMessage(ctx context.Context, msg *v1.ExportMetricsServiceRequest) (*v1.ExportMetricsServiceResponse, *status.Status, int) {
	err := s.writer.Write(ctx, msg)

	if err != nil {
		if errors.Is(err, ErrUnknownMetricType) {
			s.logger.Warn("Received unknown metric type", "Error", err)
			status := newErrorStatus("Unknown metric type")
			return nil, status, http.StatusBadRequest
		}

		// Rejected metrics are not an error - return OK with the count of rejected metrics and the error.
		var rejectedMetricsErr *ErrRejectedMetric
		if errors.As(err, &rejectedMetricsErr) {
			s.logger.Warn("Rejecting some metrics", "Error", err)
			resp := &v1.ExportMetricsServiceResponse{
				PartialSuccess: &v1.ExportMetricsPartialSuccess{
					RejectedDataPoints: rejectedMetricsErr.Count,
					ErrorMessage:       rejectedMetricsErr.Msg,
				},
			}
			return resp, nil, http.StatusOK
		}

		var writeErr *ErrWriteError
		if errors.As(err, &writeErr) {
			s.logger.Error("Failed to write metrics", "Error", err)
			status := newErrorStatus("Failed to write metrics. Please try again.")
			return nil, status, http.StatusInternalServerError
		} else {
			// Unknown error
			s.logger.Error("Failed to forward metrics with unknown error", "Error", err)
			status := newErrorStatus("Internal Server Error. Please try again.")
			return nil, status, http.StatusInternalServerError
		}
	}

	return &v1.ExportMetricsServiceResponse{}, nil, http.StatusOK
}

func writeExportMetricsServiceResponse(w http.ResponseWriter, resp *v1.ExportMetricsServiceResponse, m *prometheus.CounterVec) {
	w.Header().Add("Content-Type", "application/x-protobuf")

	respBodyBytes, err := proto.Marshal(resp)
	if err != nil {
		logger.Error("Failed to marshal response", "Error", err)
		w.WriteHeader(http.StatusInternalServerError)
		m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
		return
	}
	m.WithLabelValues(strconv.Itoa(http.StatusOK)).Inc()
	w.WriteHeader(http.StatusOK)
	w.Write(respBodyBytes)
}

func writeErrorStatusResponse(w http.ResponseWriter, statusCode int, status *status.Status, m *prometheus.CounterVec) {
	w.Header().Add("Content-Type", "application/x-protobuf")

	respBodyBytes, err := proto.Marshal(status)
	if err != nil {
		logger.Error("Failed to marshal response", "Error", err)
		w.WriteHeader(http.StatusInternalServerError)
		m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
		return
	}
	m.WithLabelValues(strconv.Itoa(statusCode)).Inc()
	w.WriteHeader(statusCode)
	w.Write(respBodyBytes)
}

func newErrorStatus(msg string) *status.Status {
	return &status.Status{
		Message: msg,
	}
}
