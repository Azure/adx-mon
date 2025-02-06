package otlp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	commonv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/otlp"
	"github.com/Azure/adx-mon/storage"
	gbp "github.com/libp2p/go-buffer-pool"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/proto"
)

type LogsServiceOpts struct {
	Store         storage.Store
	AddAttributes map[string]string
	HealthChecker interface{ IsHealthy() bool }
}

type LogsService struct {
	store         storage.Store
	logger        *slog.Logger
	healthChecker interface{ IsHealthy() bool }

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
		healthChecker:    opts.HealthChecker,
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

	if !s.healthChecker.IsHealthy() {
		m.WithLabelValues(strconv.Itoa(http.StatusTooManyRequests)).Inc()
		http.Error(w, "Overloaded. Retry later", http.StatusTooManyRequests)
		return
	}

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
		b = b[:n]

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

		logBatch := types.LogBatchPool.Get(1).(*types.LogBatch)
		logBatch.Reset()

		droppedLogMissingMetadata := s.convertToLogBatch(msg, logBatch)

		err = s.store.WriteNativeLogs(r.Context(), logBatch)
		for _, log := range logBatch.Logs {
			types.LogPool.Put(log)
		}
		types.LogBatchPool.Put(logBatch)

		if err != nil {
			m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
			s.logger.Error("Failed to write to store", "Error", err)

			w.WriteHeader(http.StatusInternalServerError)
			ret := status.Status{
				Message: "Failed to write logs",
			}
			respBodyBytes, err := proto.Marshal(&ret)
			if err == nil {
				w.Write(respBodyBytes)
			}
			return
		}

		// The logs have been committed by the OTLP endpoint
		resp := &v1.ExportLogsServiceResponse{}
		if droppedLogMissingMetadata > 0 {
			resp.SetPartialSuccess(&v1.ExportLogsPartialSuccess{
				RejectedLogRecords: droppedLogMissingMetadata,
				ErrorMessage:       "Logs lacking kube.database and kube.table attributes or body fields",
			})
			metrics.InvalidLogsDropped.WithLabelValues().Add(float64(droppedLogMissingMetadata))
		}

		respBodyBytes, err := proto.Marshal(resp)
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

var (
	ErrMissingKustoMetadata = errors.New("missing kusto metadata")
	ErrMalformedLogs        = errors.New("malformed log records")
)

// convertToLogBatch populates the LogBatch with the logs from the OTLP message. Returns the number of logs that were lacking kusto routing metadata
func (s *LogsService) convertToLogBatch(msg *v1.ExportLogsServiceRequest, logBatch *types.LogBatch) int64 {
	if msg == nil {
		return 0
	}

	var droppedLogMissingMetadata int64 = 0
	for _, resourceLog := range msg.ResourceLogs {
		if resourceLog == nil {
			continue
		}
		for _, scope := range resourceLog.ScopeLogs {
			if scope == nil {
				continue
			}
			for _, record := range scope.LogRecords {
				if record == nil {
					continue
				}

				// Senders are required to include Kusto metadata in the attributes or body
				// Must have kusto.database and kusto.table
				dbName, tableName := otlp.KustoMetadata(record)
				if dbName == "" || tableName == "" {
					if logger.IsDebug() {
						s.logger.Warn("Missing Kusto metadata", "Payload", record.String())
					}

					droppedLogMissingMetadata++
					continue
				}

				log := types.LogPool.Get(1).(*types.Log)
				log.Reset()

				log.Timestamp = record.TimeUnixNano
				log.ObservedTimestamp = record.ObservedTimeUnixNano
				extractKeyValues(record.Attributes, log.Attributes)
				extractBody(record.Body, log.Body)

				if resourceLog.Resource != nil {
					extractKeyValues(resourceLog.Resource.Attributes, log.Resource)
				}

				log.Attributes[types.AttributeDatabaseName] = dbName
				log.Attributes[types.AttributeTableName] = tableName

				metrics.LogKeys.WithLabelValues(dbName, tableName).Inc()

				logBatch.Logs = append(logBatch.Logs, log)
			}
		}
	}
	return droppedLogMissingMetadata
}

const defaultMaxDepth = 20

func extractBody(body *commonv1.AnyValue, dest map[string]any) {
	if body == nil || !body.HasValue() {
		return
	}

	if body.HasKvlistValue() {
		kvList := body.GetKvlistValue()
		if kvList == nil {
			return
		}

		extractKeyValues(kvList.Values, dest)
	} else {
		vv, ok := extract(body, 0, defaultMaxDepth)
		if ok {
			dest[types.BodyKeyMessage] = vv
		}
	}
}

func extractKeyValues(kvs []*commonv1.KeyValue, dest map[string]any) {
	if kvs == nil {
		return
	}

	for _, kv := range kvs {
		if kv == nil {
			continue
		}
		vv, ok := extract(kv.Value, 0, defaultMaxDepth)
		if !ok {
			continue
		}
		dest[kv.Key] = vv
	}
}

func extract(val *commonv1.AnyValue, depth int, maxdepth int) (value any, ok bool) {
	if val == nil {
		return nil, false
	}

	if depth > maxdepth {
		return "...", true // Just cut off here.
	}

	switch val.Value.(type) {
	case *commonv1.AnyValue_StringValue:
		return val.GetStringValue(), true
	case *commonv1.AnyValue_BoolValue:
		return val.GetBoolValue(), true
	case *commonv1.AnyValue_IntValue:
		return val.GetIntValue(), true
	case *commonv1.AnyValue_DoubleValue:
		return val.GetDoubleValue(), true
	case *commonv1.AnyValue_BytesValue:
		return val.GetBytesValue(), true
	case *commonv1.AnyValue_ArrayValue:
		arrayValue := val.GetArrayValue()
		if arrayValue == nil {
			return nil, false
		}

		ret := make([]any, 0, len(arrayValue.Values))
		for _, v := range arrayValue.Values {
			vv, ok := extract(v, depth+1, maxdepth)
			if !ok {
				continue
			}
			ret = append(ret, vv)
		}
		return ret, true
	case *commonv1.AnyValue_KvlistValue:
		kvList := val.GetKvlistValue()
		if kvList == nil {
			return nil, false
		}

		ret := map[string]any{}
		for _, kv := range kvList.Values {
			if kv == nil || !kv.HasValue() {
				continue
			}
			vv, ok := extract(kv.Value, depth+1, maxdepth)
			if !ok {
				continue
			}
			ret[kv.Key] = vv
		}
		return ret, true
	default:
		return nil, false
	}
}
