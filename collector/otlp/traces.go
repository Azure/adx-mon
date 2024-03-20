package otlp

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/trace/v1/tracev1connect"
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/trace/v1"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	connect_go "github.com/bufbuild/connect-go"
	"github.com/prometheus/client_golang/prometheus"
)

type TraceServiceOpts struct {
	Store         storage.Store
	Path          string
	AddAttributes map[string]string
}

type TraceService struct {
	path   string
	store  storage.Store
	logger *slog.Logger
}

func NewTraceService(opts TraceServiceOpts) *TraceService {
	return &TraceService{
		path:  strings.TrimSuffix(opts.Path, "/"),
		store: opts.Store,
		logger: slog.Default().With(
			slog.Group(
				"handler",
				slog.String("protocol", "otlp"),
				slog.String("type", "traces"),
			),
		),
	}
}

func (s *TraceService) Export(ctx context.Context, req *connect_go.Request[v1.ExportTraceServiceRequest]) (*connect_go.Response[v1.ExportTraceServiceResponse], error) {
	m := metrics.RequestsReceived.MustCurryWith(prometheus.Labels{"path": tracev1connect.TraceServiceExportProcedure})

	// TODO: things
	if logger.IsDebug() {
		s.logger.Debug("Received trace over gRPC")
	}

	m.WithLabelValues(strconv.Itoa(http.StatusOK)).Inc()
	return connect_go.NewResponse(&v1.ExportTraceServiceResponse{}), nil
}

func (s *TraceService) Handler(w http.ResponseWriter, r *http.Request) {
	m := metrics.RequestsReceived.MustCurryWith(prometheus.Labels{"path": s.path})
	defer r.Body.Close()

	// TODO: things
	if logger.IsDebug() {
		s.logger.Debug("Received trace over http")
	}

	m.WithLabelValues(strconv.Itoa(http.StatusOK)).Inc()
	w.WriteHeader(http.StatusNotImplemented)
}
