package otlp

import (
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	logsv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/logs/v1"
)

type GroupedLogs struct {
	Database          string
	Table             string
	ResourceSchemaURL string
	ScopeSchemaURL    string
	NumberOfRecords   int
	Logs              *v1.ExportLogsServiceRequest
}

// GroupByKustoTable receives an ExportLogsServiceRequest, which contains OTLP logs
// that have different Kusto destinations. We define a Kusto destination as a combination
// of keys `kusto.table` and `kusto.database`. This function will group logs according
// to their Kusto destination as well as their SchemaURL, which is defined by the OTLP
// specification as applying to all the contained logs.
// See https://opentelemetry.io/docs/specs/otel/schemas/#otlp-support
func GroupByKustoTable(req *v1.ExportLogsServiceRequest) []GroupedLogs {
	m := make(map[string]GroupedLogs)
	for _, r := range req.GetResourceLogs() {
		for _, s := range r.GetScopeLogs() {
			for _, l := range s.GetLogRecords() {
				var (
					table    string
					database string
					key      string
				)
				for _, a := range l.GetAttributes() {
					if a.GetKey() == "kusto.table" {
						table = a.GetValue().GetStringValue()
					}
					if a.GetKey() == "kusto.database" {
						database = a.GetValue().GetStringValue()
					}
					if table != "" && database != "" {
						break
					}
				}
				// ResourceLogs contains a Resource and a slice of ScopeLogs, where all
				// the logs therein are grouped by a common SchemaURL. We, then, will
				// include the SchemaURL in our partition key, which means our grouped
				// logs will always have a single ResourceLog with multiple ScopeLogs.
				// Likewise for ScopeLogs, we'll include the SchemaURL in our partition.
				key = database + table + r.GetSchemaUrl() + s.GetSchemaUrl()
				v, ok := m[key]
				if !ok {
					v = GroupedLogs{
						Database:          database,
						Table:             table,
						ResourceSchemaURL: r.GetSchemaUrl(),
						ScopeSchemaURL:    s.GetSchemaUrl(),
						NumberOfRecords:   1,
						Logs: &v1.ExportLogsServiceRequest{
							ResourceLogs: []*logsv1.ResourceLogs{},
						},
					}
				}
				if len(v.Logs.ResourceLogs) == 0 {
					v.Logs.ResourceLogs = append(v.Logs.ResourceLogs, &logsv1.ResourceLogs{
						Resource: r.GetResource(),
						ScopeLogs: []*logsv1.ScopeLogs{
							{
								Scope:      s.GetScope(),
								LogRecords: []*logsv1.LogRecord{l},
								SchemaUrl:  s.GetSchemaUrl(),
							},
						},
						SchemaUrl: r.GetSchemaUrl(),
					})
				} else {
					v.Logs.ResourceLogs[0].ScopeLogs[0].LogRecords = append(v.Logs.ResourceLogs[0].ScopeLogs[0].LogRecords, l)
					v.NumberOfRecords += 1
				}
				m[key] = v
			}
		}
	}
	var result []GroupedLogs
	for _, v := range m {
		result = append(result, v)
	}
	return result
}
