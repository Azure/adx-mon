package otlp

import (
	"log/slog"
	"strconv"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	commonv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	logsv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/logs/v1"
	"github.com/Azure/adx-mon/pkg/tlv"
	"github.com/prometheus/client_golang/prometheus"
)

// Logs is a collection of logs and their resources.
// https://opentelemetry.io/docs/specs/otel/logs/data-model/#field-resource
type Logs struct {
	Resources []*commonv1.KeyValue
	Logs      []*logsv1.LogRecord
	Database  string
	Table     string
}

const (
	dbKey  = "kusto.database"
	tblKey = "kusto.table"

	LogsTotalTag = tlv.Tag(0xAB)
)

func EmitMetricsForTLV(tlvs []tlv.TLV, counter *prometheus.CounterVec, database, table string) {
	for _, t := range tlvs {
		if t.Tag == LogsTotalTag {
			if v, err := strconv.Atoi(string(t.Value)); err == nil {
				counter.WithLabelValues(database, table).Add(float64(v))
			}
		}
	}
}

// Group logs into a collection of logs and their resources.
func Group(req *v1.ExportLogsServiceRequest, add []*commonv1.KeyValue, log *slog.Logger) []*Logs {
	var grouped []*Logs
	if req == nil {
		return grouped
	}

	for _, r := range req.GetResourceLogs() {
		if r == nil {
			continue
		}

		for _, s := range r.GetScopeLogs() {
			if s == nil {
				continue
			}
			for _, l := range s.GetLogRecords() {
				database, table := KustoMetadata(l)
				if database == "" || table == "" {
					// We could return an error here, but there are possibly negative
					// downstream impact to doing so. If a client is sending us logs
					// that contain no destination intermingled with those that do,
					// propogating an error downstream will cause the entire batch
					// to fail and be resent, which will cycle until the client finally
					// gives up on the batch and discards all its logs. Instead, we'll
					// log the occurance and process as much of the batch as
					// possible so the client can make forward progress.
					log.Warn("Missing Kusto metadata", "Payload", l.String())
					continue
				}

				idx := -1
				for i, g := range grouped {
					if g.Database == database && g.Table == table {
						idx = i
						break
					}
				}
				if idx == -1 {
					grouped = append(grouped, &Logs{
						Resources: append(r.GetResource().GetAttributes(), add...),
						Database:  database,
						Table:     table,
					})
					idx = len(grouped) - 1
				}

				grouped[idx].Logs = append(grouped[idx].Logs, l)
			}
		}
	}
	return grouped
}

func KustoMetadata(l *logsv1.LogRecord) (database, table string) {
	if l == nil {
		return
	}
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
	if b := l.GetBody(); b != nil {
		if lv := b.GetKvlistValue(); lv != nil {
			for _, v := range lv.GetValues() {
				switch v.GetKey() {
				case dbKey:
					database = v.GetValue().GetStringValue()
				case tblKey:
					table = v.GetValue().GetStringValue()
				}
				if database != "" && table != "" {
					return
				}
			}
		}
	}
	return
}
