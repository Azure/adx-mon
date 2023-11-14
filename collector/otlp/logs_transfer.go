package otlp

import (
	"bytes"
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
	"github.com/Azure/adx-mon/ingestor/transform"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/otlp"
	"github.com/Azure/adx-mon/pkg/pool"
	"github.com/Azure/adx-mon/pkg/tlv"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/pkg/wal/file"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"
)

var (
	csvWriterPool = pool.NewGeneric(1000, func(sz int) interface{} {
		return transform.NewCSVWriter(bytes.NewBuffer(make([]byte, 0, sz)), nil)
	})
	bufs = pool.NewBytes(1024 * 1024)
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
		b := bufs.Get(int(r.ContentLength))
		defer bufs.Put(b)

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
			err := func() error {
				metrics.LogsProxyReceived.WithLabelValues(group.Database, group.Table).Add(float64(len(group.Logs)))
				if err := s.store.WriteOTLPLogs(r.Context(), group.Database, group.Table, group); err != nil {
					return fmt.Errorf("failed to write to store: %w", err)
				}
				return nil
			}()

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

func serialize(ctx context.Context, req *v1.ExportLogsServiceRequest, add []*commonv1.KeyValue, mp *file.MemoryProvider, log *slog.Logger) ([]serialized, error) {
	var s []serialized

	enc := csvWriterPool.Get(8 * 1024).(*transform.CSVWriter)
	defer csvWriterPool.Put(enc)

	grouped := otlp.Group(req, add, log)
	for _, group := range grouped {
		metrics.LogsProxyReceived.WithLabelValues(group.Database, group.Table).Add(float64(len(group.Logs)))
		w, err := wal.NewWAL(wal.WALOpts{StorageProvider: mp, Prefix: fmt.Sprintf("%s_%s", group.Database, group.Table)})
		if err != nil {
			return nil, fmt.Errorf("failed to create wal: %w", err)
		}
		if err := w.Open(ctx); err != nil {
			return nil, fmt.Errorf("failed to open wal: %w", err)
		}
		enc.Reset()
		if err := enc.MarshalCSV(group); err != nil {
			return nil, fmt.Errorf("failed to marshal csv: %w", err)
		}
		// Add our TLV metadata
		b := enc.Bytes()
		tNumLogs := tlv.New(otlp.LogsTotalTag, []byte(strconv.Itoa(len(group.Logs))))
		tPayloadSize := tlv.New(tlv.PayloadTag, []byte(strconv.Itoa(len(b))))
		if err := w.Write(ctx, append(tlv.Encode(tNumLogs, tPayloadSize), b...)); err != nil {
			return nil, fmt.Errorf("failed to write to wal: %w", err)
		}
		s = append(s, serialized{Path: w.Path(), Database: group.Database, Table: group.Table, Logs: len(group.Logs)})
		if err := w.Close(); err != nil {
			return nil, fmt.Errorf("failed to close wal: %w", err)
		}
	}

	return s, nil
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
