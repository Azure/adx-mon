package otlp

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/logs/v1/logsv1connect"
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	v11 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	logsv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/logs/v1"
	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/pool"
	connect_go "github.com/bufbuild/connect-go"
	"github.com/prometheus/client_golang/prometheus"
)

var bytesPool = pool.NewBytes(1024)

type logsServer struct {
	w cluster.OTLPLogsWriter
}

func NewLogsServer(w cluster.OTLPLogsWriter) logsv1connect.LogsServiceHandler {
	logger.Info("Initializing OTLP logs server")
	return &logsServer{w: w}
}

func (srv *logsServer) Export(ctx context.Context, req *connect_go.Request[v1.ExportLogsServiceRequest]) (*connect_go.Response[v1.ExportLogsServiceResponse], error) {
	var (
		m    = metrics.RequestsReceived.MustCurryWith(prometheus.Labels{"path": logsv1connect.LogsServiceExportProcedure})
		d, t string
	)

	for key, logs := range groupByKustoTable(req.Msg) {
		d, t = metadataFromKey(key)
		if d == "" || t == "" {
			m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
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
			m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
			res := &v1.ExportLogsServiceResponse{
				PartialSuccess: &v1.ExportLogsPartialSuccess{
					RejectedLogRecords: int64(len(logs)),
					ErrorMessage:       err.Error(),
				},
			}
			return connect_go.NewResponse(res), err
		}
		metrics.LogsReceived.WithLabelValues(d, t).Add(float64(len(logs)))
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
func groupByKustoTable(req *v1.ExportLogsServiceRequest) map[string][]*logsv1.LogRecord {
	b := bytesPool.Get(1024)
	defer bytesPool.Put(b)

	var (
		d, t, k string
		a       []*v11.KeyValue
		m       = make(map[string][]*logsv1.LogRecord)
	)
	for _, r := range req.GetResourceLogs() {
		a = r.Resource.GetAttributes()
		for _, s := range r.GetScopeLogs() {
			for _, l := range s.GetLogRecords() {
				// Each LogRecord contains Attributes that we merge
				// with the ResourceLog's Attributes, which come from
				// Collector's --add-attributes.
				l.Attributes = append(l.Attributes, a...)
				// Extract the destination Kusto Database and Table
				d, t = kustoMetadata(l)
				b = makeKey(b[:0], d, t)
				k = string(b)
				m[k] = append(m[k], l)
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
