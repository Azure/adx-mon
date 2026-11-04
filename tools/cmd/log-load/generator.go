package main

import (
	"io"
	"time"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	commonv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	otelv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/logs/v1"
	resourcev1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/resource/v1"
)

type continuousDataGenerator struct {
	batchSize    int
	totalSamples int64
	sent         int64
	database     string
	table        string
}

func (c *continuousDataGenerator) Read() (*v1.ExportLogsServiceRequest, error) {
	if c.totalSamples > 0 && c.sent >= c.totalSamples {
		return nil, io.EOF
	}
	size := c.batchSize
	if c.totalSamples > 0 {
		size = int(min(int64(size), c.totalSamples-c.sent))
	}
	msg := newExportLogRequest()
	logs := make([]*otelv1.LogRecord, 0, size)
	for i := 0; i < size; i++ {
		logs = append(logs, &otelv1.LogRecord{
			TimeUnixNano: uint64(time.Now().UnixNano()), SeverityNumber: 17, SeverityText: "Error",
			Body: &commonv1.AnyValue{Value: &commonv1.AnyValue_KvlistValue{
				KvlistValue: &commonv1.KeyValueList{Values: []*commonv1.KeyValue{
					stringValue("message", "something worth logging"),
					stringValue("kusto.table", c.table),
					stringValue("kusto.database", c.database),
				}},
			}},
			DroppedAttributesCount: 1, Flags: 1,
		})
	}
	msg.ResourceLogs[0].ScopeLogs[0].LogRecords = logs
	c.sent += int64(size)
	return msg, nil
}

func stringValue(key, value string) *commonv1.KeyValue {
	return &commonv1.KeyValue{Key: key, Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: value}}}
}

func recordCount(batch *v1.ExportLogsServiceRequest) int64 {
	var n int64
	for _, resource := range batch.GetResourceLogs() {
		for _, scope := range resource.GetScopeLogs() {
			n += int64(len(scope.GetLogRecords()))
		}
	}
	return n
}

func newExportLogRequest() *v1.ExportLogsServiceRequest {
	return &v1.ExportLogsServiceRequest{
		ResourceLogs: []*otelv1.ResourceLogs{{
			Resource: &resourcev1.Resource{
				Attributes: []*commonv1.KeyValue{stringValue("source", "hostname")}, DroppedAttributesCount: 1,
			},
			ScopeLogs: []*otelv1.ScopeLogs{{
				Scope: &commonv1.InstrumentationScope{Name: "name", Version: "version", DroppedAttributesCount: 1},
			}},
		}},
	}
}
