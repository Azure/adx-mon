package otlp

import (
	"context"
	"os"
	"testing"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	commonv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestAttributes(t *testing.T) {
	var log v1.ExportLogsServiceRequest
	if err := protojson.Unmarshal(rawlog, &log); err != nil {
		require.NoError(t, err)
	}

	add := []*commonv1.KeyValue{
		{
			Key: "SomeAttribute",
			Value: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_StringValue{
					StringValue: "SomeValue",
				},
			},
		},
	}
	lift := map[string]struct{}{
		"kusto.table":    {},
		"kusto.database": {},
	}

	modified, err := modifyAttributes(&log, add, lift)
	require.NoError(t, err)

	require.Equal(t, 1, len(modified.ResourceLogs[0].ScopeLogs[0].LogRecords[0].Body.GetKvlistValue().Values))
	require.Equal(t, "message", modified.ResourceLogs[0].ScopeLogs[0].LogRecords[0].Body.GetKvlistValue().Values[0].Key)

	require.Equal(t, 2, len(modified.ResourceLogs[0].Resource.Attributes))
	require.Equal(t, "source", modified.ResourceLogs[0].Resource.Attributes[0].Key)
	require.Equal(t, "hostname", modified.ResourceLogs[0].Resource.Attributes[0].Value.GetStringValue())

	require.Equal(t, "SomeAttribute", modified.ResourceLogs[0].Resource.Attributes[1].Key)
	require.Equal(t, "SomeValue", modified.ResourceLogs[0].Resource.Attributes[1].Value.GetStringValue())

	require.Equal(t, 2, len(modified.ResourceLogs[0].ScopeLogs[0].LogRecords[0].Attributes))
	require.Equal(t, "kusto.table", modified.ResourceLogs[0].ScopeLogs[0].LogRecords[0].Attributes[0].Key)
	require.Equal(t, "ATable", modified.ResourceLogs[0].ScopeLogs[0].LogRecords[0].Attributes[0].Value.GetStringValue())

	require.Equal(t, "kusto.database", modified.ResourceLogs[0].ScopeLogs[0].LogRecords[0].Attributes[1].Key)
	require.Equal(t, "ADatabase", modified.ResourceLogs[0].ScopeLogs[0].LogRecords[0].Attributes[1].Value.GetStringValue())
}

func TestSerializedLogs(t *testing.T) {
	var log v1.ExportLogsServiceRequest
	if err := protojson.Unmarshal(rawlog, &log); err != nil {
		require.NoError(t, err)
	}

	add := []*commonv1.KeyValue{
		{
			Key: "SomeAttribute",
			Value: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_StringValue{
					StringValue: "SomeValue",
				},
			},
		},
	}

	dir := t.TempDir()
	wals, err := serializedLogs(context.Background(), &log, add, dir)
	require.NoError(t, err)

	require.Equal(t, 1, len(wals))

	for _, w := range wals {
		b, err := os.ReadFile(w)
		require.NoError(t, err)
		require.NotEqual(t, 0, len(b))
	}
}

func BenchmarkSerializedLogs(b *testing.B) {
	var log v1.ExportLogsServiceRequest
	if err := protojson.Unmarshal(rawlog, &log); err != nil {
		require.NoError(b, err)
	}

	dir := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := serializedLogs(context.Background(), &log, nil, dir)
		require.NoError(b, err)
	}
}

func BenchmarkModifyAttributes(b *testing.B) {
	var log v1.ExportLogsServiceRequest
	if err := protojson.Unmarshal(rawlog, &log); err != nil {
		require.NoError(b, err)
	}

	add := []*commonv1.KeyValue{
		{
			Key: "SomeAttribute",
			Value: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_StringValue{
					StringValue: "SomeValue",
				},
			},
		},
	}
	lift := map[string]struct{}{
		"kusto.table":    {},
		"kusto.database": {},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		modifyAttributes(&log, add, lift)
	}
}

var rawlog = []byte(`{
	"resourceLogs": [
		{
			"resource": {
				"attributes": [
					{
						"key": "source",
						"value": {
							"stringValue": "hostname"
						}
					}
				],
				"droppedAttributesCount": 1
			},
			"scopeLogs": [
				{
					"scope": {
						"name": "name",
						"version": "version",
						"droppedAttributesCount": 1
					},
					"logRecords": [
						{
							"timeUnixNano": "1669112524001",
							"observedTimeUnixNano": "1669112524001",
							"severityNumber": 17,
							"severityText": "Error",
							"body": {
								"kvlistValue": {
									"values": [
									  {
										"key": "message",
										"value": {
										  "stringValue": "something worth logging"
										}
									  },
									  {
										"key": "kusto.table",
										"value": {
										  "stringValue": "ATable"
										}
									  },
									  {
										"key": "kusto.database",
										"value": {
										  "stringValue": "ADatabase"
										}
									  }
									]
								  }
							},
							"droppedAttributesCount": 1,
							"flags": 1,
							"traceId": "",
							"spanId": ""
						}
					],
					"schemaUrl": "scope_schema"
				}
			],
			"schemaUrl": "resource_schema"
		}
	]
}`)
