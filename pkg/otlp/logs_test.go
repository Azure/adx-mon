package otlp_test

import (
	"log/slog"
	"testing"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	"github.com/Azure/adx-mon/pkg/otlp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestGroup(t *testing.T) {
	var log v1.ExportLogsServiceRequest
	err := protojson.Unmarshal(otlpLog, &log)
	require.NoError(t, err)

	grouped := otlp.Group(&log, nil, slog.Default())
	require.Equal(t, 2, len(grouped))
	for _, g := range grouped {
		switch g.Table {
		case "ATable":
			require.Equal(t, "ADatabase", g.Database)
			require.Equal(t, 2, len(g.Logs))
		case "BTable":
			require.Equal(t, "ADatabase", g.Database)
			require.Equal(t, 1, len(g.Logs))
		default:
			require.Fail(t, "unexpected table", g.Table)
		}
	}
}

var otlpLog = []byte(`{
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
								"stringValue": "{\"msg\":\"something happened\n\"}"
							},
							"attributes": [
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
							],
							"droppedAttributesCount": 1,
							"flags": 1,
							"traceId": "",
							"spanId": ""
						},
						{
							"timeUnixNano": "1669112524001",
							"observedTimeUnixNano": "1669112524001",
							"severityNumber": 17,
							"severityText": "Error",
							"body": {
								"stringValue": "{\"msg\":\"something happened\n\"}"
							},
							"attributes": [
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
							],
							"droppedAttributesCount": 1,
							"flags": 1,
							"traceId": "",
							"spanId": ""
						},
						{
							"timeUnixNano": "1669112524001",
							"observedTimeUnixNano": "1669112524001",
							"severityNumber": 17,
							"severityText": "Error",
							"body": {
								"stringValue": "{\"msg\":\"something happened\n\"}"
							},
							"attributes": [
								{
									"key": "kusto.table",
									"value": {
										"stringValue": "BTable"
									}
								},
								{
									"key": "kusto.database",
									"value": {
										"stringValue": "ADatabase"
									}
								}
							],
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
