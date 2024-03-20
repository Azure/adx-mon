package otlp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	commonv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/otlp"
	gbp "github.com/libp2p/go-buffer-pool"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"
)

type LogsServiceOpts struct {
	Store         storage.Store
	AddAttributes map[string]string
}

type LogsService struct {
	store  storage.Store
	logger *slog.Logger

	staticAttributes []*commonv1.KeyValue
}

func NewLogsService(opts LogsServiceOpts) *LogsService {
	var add []*commonv1.KeyValue
	for attribute, value := range opts.AddAttributes {
		add = append(add, &commonv1.KeyValue{
			Key: attribute,
			Value: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_StringValue{
					StringValue: value,
				},
			},
		},
		)
	}

	return &LogsService{
		store: opts.Store,
		logger: slog.Default().With(
			slog.Group(
				"handler",
				slog.String("protocol", "otlp"),
				slog.String("type", "logs"),
			),
		),
		staticAttributes: add,
	}
}

func (s *LogsService) Open(ctx context.Context) error {
	return nil
}

func (s *LogsService) Close() error {
	return nil
}

func (s *LogsService) Handler(w http.ResponseWriter, r *http.Request) {
	m := metrics.RequestsReceived.MustCurryWith(prometheus.Labels{"path": "/v1/logs"})
	defer r.Body.Close()

	switch r.Header.Get("Content-Type") {
	case "application/x-protobuf":

		// Consume the request body and marshal into a protobuf
		b := gbp.Get(int(r.ContentLength))
		defer gbp.Put(b)

		n, err := io.ReadFull(r.Body, b)
		if err != nil {
			s.logger.Error("Failed to read request body", "Error", err)
			w.WriteHeader(http.StatusInternalServerError)
			m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
			return
		}
		if n < int(r.ContentLength) {
			s.logger.Warn("Short read")
			w.WriteHeader(http.StatusBadRequest)
			m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
			return
		}
		if logger.IsDebug() {
			s.logger.Debug("Received request body", "Bytes", n)
		}

		msg := &v1.ExportLogsServiceRequest{}
		if err := proto.Unmarshal(b, msg); err != nil {
			s.logger.Error("Failed to unmarshal request body", "Error", err)
			w.WriteHeader(http.StatusBadRequest)
			m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
			return
		}

		grouped := otlp.Group(msg, s.staticAttributes, s.logger)
		for _, group := range grouped {
			err := func(g *otlp.Logs) error {
				if err := s.store.WriteOTLPLogs(r.Context(), g.Database, g.Table, g); err != nil {
					return fmt.Errorf("failed to write to store: %w", err)
				}
				metrics.LogsProxyReceived.WithLabelValues(g.Database, g.Table).Add(float64(len(g.Logs)))
				metrics.LogKeys.WithLabelValues(g.Database, g.Table).Add(float64(len(g.Logs)))
				metrics.LogSize.WithLabelValues(g.Database, g.Table).Add(float64(sizeofLogsInGroup(g)))
				return nil
			}(group)

			// Serialize the logs into a WAL, grouped by Kusto destination database and table
			if isUserError(err) {
				s.logger.Error("Failed to serialize logs with user error", "Error", err)
				w.WriteHeader(http.StatusBadRequest)
				m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
				return
			}
			if err != nil {
				s.logger.Error("Failed to serialize logs with internal error", "Error", err)
				w.WriteHeader(http.StatusInternalServerError)
				m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
				return
			}
		}

		// The logs have been committed by the OTLP endpoint
		respBodyBytes, err := proto.Marshal(&v1.ExportLogsServiceResponse{})
		if err != nil {
			s.logger.Error("Failed to marshal response", "Error", err)
			w.WriteHeader(http.StatusInternalServerError)
			m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
			return
		}

		// Even in the case of a partial response, OTLP API requires us to send StatusOK
		w.Header().Add("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
		w.Write(respBodyBytes)
		m.WithLabelValues(strconv.Itoa(http.StatusOK)).Inc()

	case "application/json":
		// We're receiving JSON, so we need to unmarshal the JSON
		// into an OTLP protobuf, then use gRPC to send the OTLP
		// protobuf to the OTLP endpoint
		w.WriteHeader(http.StatusUnsupportedMediaType)
		m.WithLabelValues(strconv.Itoa(http.StatusUnsupportedMediaType)).Inc()

	default:
		logger.Errorf("Unsupported Content-Type: %s", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusUnsupportedMediaType)
		m.WithLabelValues(strconv.Itoa(http.StatusUnsupportedMediaType)).Inc()
	}

}

type serialized struct {
	Path     string
	Database string
	Table    string
	Logs     int
}

var (
	ErrMissingKustoMetadata = errors.New("missing kusto metadata")
	ErrMalformedLogs        = errors.New("malformed log records")
)

func isUserError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, ErrMissingKustoMetadata) {
		return true
	}

	if errors.Is(err, ErrMalformedLogs) {
		return true
	}

	return false
}

func sizeofLogsInGroup(group *otlp.Logs) int {
	var size int
	for _, log := range group.Logs {
		if log == nil {
			continue
		}
		size += proto.Size(log.GetBody())
	}
	return size
}
