package otlp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/logs/v1/logsv1connect"
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	logsv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/logs/v1"
	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/pool"
	connect_go "github.com/bufbuild/connect-go"
)

var bytesPool = pool.NewBytes(1024)

type logsServer struct {
	w         cluster.OTLPLogsWriter
	databases map[string]struct{}
}

func NewLogsServer(w cluster.OTLPLogsWriter, logsDatabases []string) logsv1connect.LogsServiceHandler {
	logger.Infof("Initializing OTLP logs server with databases: %v", logsDatabases)
	databases := make(map[string]struct{})
	for _, d := range logsDatabases {
		databases[d] = struct{}{}
	}
	return &logsServer{w: w, databases: databases}
}

func (srv *logsServer) Export(ctx context.Context, req *connect_go.Request[v1.ExportLogsServiceRequest]) (*connect_go.Response[v1.ExportLogsServiceResponse], error) {
	var (
		d, t               string
		invalidLogErrors   error = nil
		rejectedLogRecords int64 = 0
	)

	for key, logs := range groupByKustoTable(req.Msg) {
		d, t = metadataFromKey(key)
		if logger.IsDebug() {
			logger.Debugf("LogHandler received %d logs for %s.%s", len(logs), d, t)
		}

		if d == "" || t == "" {
			invalidCount := len(logs)
			invalidLogErrors = errors.Join(invalidLogErrors, fmt.Errorf("request missing destination metadata for %d logs", invalidCount))
			rejectedLogRecords += int64(invalidCount)
		} else if _, ok := srv.databases[d]; !ok {
			invalidCount := len(logs)
			invalidLogErrors = errors.Join(invalidLogErrors, fmt.Errorf("request destination not supported for %d logs: %s", invalidCount, d))
			rejectedLogRecords += int64(invalidCount)
		} else if err := srv.w(ctx, d, t, logs); err != nil {
			// Internal errors with writer. Bail out now and allow the client to retry.
			logger.Errorf("Failed to write logs to %s.%s: %v", d, t, err)
			metrics.ValidLogsDropped.WithLabelValues().Add(float64(len(logs)))

			err := errors.New("request missing destination metadata")
			res := &v1.ExportLogsServiceResponse{
				PartialSuccess: &v1.ExportLogsPartialSuccess{
					RejectedLogRecords: int64(len(logs)),
					ErrorMessage:       err.Error(),
				},
			}
			return connect_go.NewResponse(res), err
		}
		if err := srv.w(ctx, d, t, logs); err != nil {
			res := &v1.ExportLogsServiceResponse{
				PartialSuccess: &v1.ExportLogsPartialSuccess{
					RejectedLogRecords: int64(len(logs)),
					ErrorMessage:       err.Error(),
				},
			}
			return connect_go.NewResponse(res), connect_go.NewError(connect_go.CodeDataLoss, err)
		} else {
			metrics.LogsReceived.WithLabelValues(d, t).Add(float64(len(logs)))
		}
	}

	if invalidLogErrors != nil {
		logger.Warnf("Invalid logs received from %s: %v", req.Peer().Addr, invalidLogErrors)
		metrics.InvalidLogsDropped.WithLabelValues().Add(float64(rejectedLogRecords))
		res := &v1.ExportLogsServiceResponse{
			PartialSuccess: &v1.ExportLogsPartialSuccess{
				RejectedLogRecords: rejectedLogRecords,
				ErrorMessage:       invalidLogErrors.Error(),
			},
		}
		return connect_go.NewResponse(res), connect_go.NewError(connect_go.CodeInvalidArgument, invalidLogErrors)
	}

	return connect_go.NewResponse(&v1.ExportLogsServiceResponse{}), nil
}

func makeKey(dst []byte, database, table string) []byte {
	dst = append(dst, []byte(database)...)
	dst = append(dst, sep...)
	return append(dst, []byte(table)...)
}

func metadataFromKey(key string) (database, table string) {
	ss := strings.Split(key, string(sep))
	if len(ss) != 2 {
		return
	}
	database, table = ss[0], ss[1]
	return
}

var sep = []byte("_")

// groupByKustoTable receives an ExportLogsServiceRequest, which contains OTLP logs
// that have different Kusto destinations. We define a Kusto destination as a combination
// of keys `kusto.table` and `kusto.database`. This function will group logs according
// to their Kusto destination as well as their SchemaURL, which is defined by the OTLP
// specification as applying to all the contained logs.
// See https://opentelemetry.io/docs/specs/otel/schemas/#otlp-support
func groupByKustoTable(req *v1.ExportLogsServiceRequest) map[string][]*logsv1.LogRecord {
	b := bytesPool.Get(1024)
	defer bytesPool.Put(b)

	var (
		d, t string
		m    = make(map[string][]*logsv1.LogRecord)
	)
	for _, r := range req.GetResourceLogs() {
		for _, s := range r.GetScopeLogs() {
			for _, l := range s.GetLogRecords() {
				// Extract the destination Kusto Database and Table
				d, t = kustoMetadata(l)
				b = makeKey(b[:0], d, t)
				m[string(b)] = append(m[string(b)], l)
			}
		}
	}
	return m
}

const (
	dbKey  = "kusto.database"
	tblKey = "kusto.table"
)

func kustoMetadata(l *logsv1.LogRecord) (database, table string) {
	for _, a := range l.GetAttributes() {
		switch a.GetKey() {
		case dbKey:
			database = a.GetValue().GetStringValue()
		case tblKey:
			table = a.GetValue().GetStringValue()
		}
		if database != "" && table != "" {
			return
		}
	}
	return
}
