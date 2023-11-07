package otlp

import (
	commonv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	logsv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/logs/v1"
)

// Logs is a collection of logs and their resources.
// https://opentelemetry.io/docs/specs/otel/logs/data-model/#field-resource
type Logs struct {
	Resources []*commonv1.KeyValue
	Logs      []*logsv1.LogRecord
}

const (
	dbKey  = "kusto.database"
	tblKey = "kusto.table"

	LogsTotalTag = tlv.Tag(0xAB)
)

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
