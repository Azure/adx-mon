package otlp_test

import (
	"testing"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	"github.com/Azure/adx-mon/pkg/otlp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestGroupbyKustoTable(t *testing.T) {
	rawlog := []byte(`{
		"resourceLogs": [
			{
				"resource": {
					"attributes": [
						{
							"key": "RPTenant",
							"value": {
								"stringValue": "eastus"
							}
						},
						{
							"key": "UnderlayName",
							"value": {
								"stringValue": "hcp-underlay-eastus-cx-test"
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
									"stringValue": "{\"msg\":\"something happened\"}"
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
							}
						],
						"schemaUrl": "scope_schema"
					},
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
									"stringValue": "{\"msg\":\"something happened\"}"
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
					},
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
									"stringValue": "{\"msg\":\"something else happened\"}"
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
							}
						],
						"schemaUrl": "scope_schema"
					}
				],
				"schemaUrl": "resource_schema"
			}
		]
	}`)
	var log v1.ExportLogsServiceRequest
	if err := protojson.Unmarshal(rawlog, &log); err != nil {
		require.NoError(t, err)
	}

	grouped := otlp.GroupByKustoTable(&log)
	require.Len(t, grouped, 2)

	for _, gl := range grouped {
		switch gl.Table {
		case "ATable":
			require.Len(t, gl.Logs.ResourceLogs, 1)
			require.Len(t, gl.Logs.ResourceLogs[0].ScopeLogs, 1)
			require.Len(t, gl.Logs.ResourceLogs[0].ScopeLogs[0].LogRecords, 2)
			require.Equal(t, gl.NumberOfRecords, 2)
			// Contains RPTenant and UnderlayName
			require.Len(t, gl.Logs.ResourceLogs[0].Resource.Attributes, 2)
		case "BTable":
			require.Len(t, gl.Logs.ResourceLogs, 1)
			require.Len(t, gl.Logs.ResourceLogs[0].ScopeLogs, 1)
			require.Len(t, gl.Logs.ResourceLogs[0].ScopeLogs[0].LogRecords, 1)
			require.Equal(t, gl.NumberOfRecords, 1)
			// Contains RPTenant and UnderlayName
			require.Len(t, gl.Logs.ResourceLogs[0].Resource.Attributes, 2)
		}
	}
}

func BenchmarkGroupByKustoTable(b *testing.B) {
	rawlog := []byte(`{
		"resourceLogs": [
			{
				"resource": {
					"attributes": [
						{
							"key": "RPTenant",
							"value": {
								"stringValue": "eastus"
							}
						},
						{
							"key": "UnderlayName",
							"value": {
								"stringValue": "hcp-underlay-eastus-cx-test"
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
									"stringValue": "{\"msg\":\"something happened\"}"
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
							}
						],
						"schemaUrl": "scope_schema"
					}
				],
				"schemaUrl": "resource_schema"
			}
		]
	}`)
	var log v1.ExportLogsServiceRequest
	if err := protojson.Unmarshal(rawlog, &log); err != nil {
		require.NoError(b, err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		otlp.GroupByKustoTable(&log)
	}
}
