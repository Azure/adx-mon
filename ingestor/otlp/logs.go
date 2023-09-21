package otlp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/logs/v1/logsv1connect"
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	logsv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/logs/v1"
	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/otlp"
	"github.com/Azure/adx-mon/pkg/pool"
	connect_go "github.com/bufbuild/connect-go"
	"github.com/prometheus/client_golang/prometheus"
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
		m                  = metrics.RequestsReceived.MustCurryWith(prometheus.Labels{"path": logsv1connect.LogsServiceExportProcedure})
		d, t               string
		invalidLogErrors   error = nil
		rejectedLogRecords int64 = 0
	)

	for key, logs := range groupByKustoTable(req.Msg) {
		d, t = metadataFromKey(key)
		if logger.IsDebug() {
			logger.Debugf("LogHandler received %d logs for %s.%s", len(logs.Logs), d, t)
		}

		if d == "" || t == "" {
			invalidCount := len(logs.Logs)
			invalidLogErrors = errors.Join(invalidLogErrors, fmt.Errorf("request missing destination metadata for %d logs, key=%s", invalidCount, key))
			rejectedLogRecords += int64(invalidCount)
		} else if _, ok := srv.databases[d]; !ok {
			invalidCount := len(logs.Logs)
			invalidLogErrors = errors.Join(invalidLogErrors, fmt.Errorf("request destination not supported for %d logs: %s", invalidCount, d))
			rejectedLogRecords += int64(invalidCount)
		} else if err := srv.w(ctx, d, t, logs); err != nil {
			// Internal errors with writer. Bail out now and allow the client to retry.
			logger.Errorf("Failed to write logs to %s.%s: %v", d, t, err)
			m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
			metrics.ValidLogsDropped.WithLabelValues().Add(float64(len(logs.Logs)))
			res := &v1.ExportLogsServiceResponse{
				PartialSuccess: &v1.ExportLogsPartialSuccess{
					RejectedLogRecords: int64(len(logs.Logs)),
					ErrorMessage:       err.Error(),
				},
			}
			return connect_go.NewResponse(res), connect_go.NewError(connect_go.CodeDataLoss, err)
		} else {
			metrics.LogsReceived.WithLabelValues(d, t).Add(float64(len(logs.Logs)))
		}
	}

	if invalidLogErrors != nil {
		logger.Warnf("Invalid logs received from %s: %v", req.Peer().Addr, invalidLogErrors)
		m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
		metrics.InvalidLogsDropped.WithLabelValues().Add(float64(rejectedLogRecords))
		res := &v1.ExportLogsServiceResponse{
			PartialSuccess: &v1.ExportLogsPartialSuccess{
				RejectedLogRecords: rejectedLogRecords,
				ErrorMessage:       invalidLogErrors.Error(),
			},
		}
		return connect_go.NewResponse(res), connect_go.NewError(connect_go.CodeInvalidArgument, invalidLogErrors)
	}

	m.WithLabelValues(strconv.Itoa(http.StatusOK)).Inc()
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
func groupByKustoTable(req *v1.ExportLogsServiceRequest) map[string]*otlp.Logs {
	b := bytesPool.Get(1024)
	defer bytesPool.Put(b)

	var (
		d, t string
		m    = make(map[string]*otlp.Logs)
	)
	for _, r := range req.GetResourceLogs() {
		for _, s := range r.GetScopeLogs() {
			for _, l := range s.GetLogRecords() {
				// Extract the destination Kusto Database and Table
				d, t = kustoMetadata(l)
				if (d == "" || t == "") && logger.IsDebug() {
					logger.Debugf("Log missing destination metadata: %s", l.String())
				}
				b = makeKey(b[:0], d, t)
				v, ok := m[string(b)]
				if !ok {
					v = &otlp.Logs{}
					// The naming here is a bit confusing, but the spec defines this particular mapping
					// in the logs data model
					if r.Resource != nil {
						v.Resources = r.Resource.Attributes
					}
				}
				v.Logs = append(v.Logs, l)
				m[string(b)] = v
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
