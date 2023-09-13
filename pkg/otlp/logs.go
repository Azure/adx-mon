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
