package otlp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/logs/v1/logsv1connect"
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	logsv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/logs/v1"
	"github.com/bufbuild/connect-go"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

type writer struct {
	t      *testing.T
	called int
	err    error
}

func (w *writer) WriteLogRecords(ctx context.Context, database, table string, logs []*logsv1.LogRecord) error {
	w.t.Helper()
	w.called++
	return w.err
}

func TestOTLP(t *testing.T) {
	w := &writer{t: t}

	mux := http.NewServeMux()
	mux.Handle(logsv1connect.NewLogsServiceHandler(NewLogsServer(w.WriteLogRecords, []string{"ADatabase", "BDatabase"})))

	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	var log v1.ExportLogsServiceRequest
	if err := protojson.Unmarshal(rawlog, &log); err != nil {
		require.NoError(t, err)
	}

	client := logsv1connect.NewLogsServiceClient(srv.Client(), srv.URL, connect.WithGRPC())
	resp, err := client.Export(context.Background(), connect.NewRequest(&log))
	require.NoError(t, err)

	if resp.Msg.GetPartialSuccess() != nil {
		t.Fatal("Did not expect a partial success")
	}
	require.Equal(t, 3, w.called)
}

// Only configure LogsServer to know about ADatabase, excluding BDatabase
func TestUnknownDestination(t *testing.T) {
	w := &writer{t: t}

	mux := http.NewServeMux()
	mux.Handle(logsv1connect.NewLogsServiceHandler(NewLogsServer(w.WriteLogRecords, []string{"ADatabase"})))

	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	var log v1.ExportLogsServiceRequest
	if err := protojson.Unmarshal(rawlog, &log); err != nil {
		require.NoError(t, err)
	}

	client := logsv1connect.NewLogsServiceClient(srv.Client(), srv.URL, connect.WithGRPC())
	_, err := client.Export(context.Background(), connect.NewRequest(&log))
	require.Error(t, err)
	var errInvalidArgument *connect.Error
	require.True(t, errors.As(err, &errInvalidArgument))
	require.Equal(t, connect.CodeInvalidArgument, errInvalidArgument.Code())

	// Expect writes to the two valid tables from ADatabase
	require.Equal(t, 2, w.called)
}

// Logs contain missing attributes for tables or databases
func TestUnconfiguredDestination(t *testing.T) {
	w := &writer{t: t}

	mux := http.NewServeMux()
	mux.Handle(logsv1connect.NewLogsServiceHandler(NewLogsServer(w.WriteLogRecords, []string{"ADatabase", "BDatabase"})))

	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	var log v1.ExportLogsServiceRequest
	if err := protojson.Unmarshal(rawlogMissingDestinations, &log); err != nil {
		require.NoError(t, err)
	}

	client := logsv1connect.NewLogsServiceClient(srv.Client(), srv.URL, connect.WithGRPC())
	_, err := client.Export(context.Background(), connect.NewRequest(&log))
	require.Error(t, err)
	var errInvalidArgument *connect.Error
	require.True(t, errors.As(err, &errInvalidArgument))
	require.Equal(t, connect.CodeInvalidArgument, errInvalidArgument.Code())

	// Expect writes from the two valid logs for the two different destinations.
	require.Equal(t, 2, w.called)
}

func TestOTLPWriterFailures(t *testing.T) {
	w := &writer{t: t, err: errors.New("something happened")}

	mux := http.NewServeMux()
	mux.Handle(logsv1connect.NewLogsServiceHandler(NewLogsServer(w.WriteLogRecords, []string{"ADatabase", "BDatabase"})))

	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	var log v1.ExportLogsServiceRequest
	if err := protojson.Unmarshal(rawlog, &log); err != nil {
		require.NoError(t, err)
	}

	client := logsv1connect.NewLogsServiceClient(srv.Client(), srv.URL, connect.WithGRPC())
	_, err := client.Export(context.Background(), connect.NewRequest(&log))
	// Since the request payload, in this case, contains the necessary destination metadata,
	// we're forcing the underlying writer, which in our case returns a failure, and from the
	// calling code's perspective is interpretted as a failure to write to the WAL, the appropriate
	// error is a write failure.
	var connectErr *connect.Error
	require.True(t, errors.As(err, &connectErr))
	require.Equal(t, connect.CodeDataLoss, connectErr.Code())
}

func TestGroupbyKustoTable(t *testing.T) {

	var log v1.ExportLogsServiceRequest
	if err := protojson.Unmarshal(rawlog, &log); err != nil {
		require.NoError(t, err)
	}

	grouped := groupByKustoTable(&log)
	require.Len(t, grouped, 3)

	for key, logs := range grouped {
		database, table := metadataFromKey(key)

		switch table {
		case "ATable":
			require.Len(t, logs, 2)
			require.Equal(t, 2, len(logs[0].Attributes))
			require.Equal(t, "ADatabase", database)
		case "BTable":
			require.Len(t, logs, 1)
			require.Equal(t, 2, len(logs[0].Attributes))
			require.Equal(t, "ADatabase", database)
		case "CTable":
			require.Len(t, logs, 1)
			require.Equal(t, 2, len(logs[0].Attributes))
			require.Equal(t, "BDatabase", database)
		default:
			require.Fail(t, "unknown table")
		}
	}
}

func BenchmarkGroupByKustoTable(b *testing.B) {

	var log v1.ExportLogsServiceRequest
	if err := protojson.Unmarshal(rawlog, &log); err != nil {
		require.NoError(b, err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		groupByKustoTable(&log)
	}
}

func BenchmarkKustoMetadata(b *testing.B) {
	var log v1.ExportLogsServiceRequest
	if err := protojson.Unmarshal(rawlog, &log); err != nil {
		require.NoError(b, err)
	}
	l := log.ResourceLogs[0].ScopeLogs[0].LogRecords[0]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		kustoMetadata(l)
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
						},
						{
							"timeUnixNano": "1669112524002",
							"observedTimeUnixNano": "1669112524002",
							"severityNumber": 17,
							"severityText": "Error",
							"body": {
								"stringValue": "{\"msg\":\"something else happened\"}"
							},
							"attributes": [
								{
									"key": "kusto.table",
									"value": {
										"stringValue": "CTable"
									}
								},
								{
									"key": "kusto.database",
									"value": {
										"stringValue": "BDatabase"
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

// First log missing kusto.table attribute.
// Third log missing kusto.database attribute.
// Two valid logs. One for ADatabase/BTable and one for BDatabase/CTable.
var rawlogMissingDestinations = []byte(`{
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
								"stringValue": "{\"msg\":\"something happened\"}"
							},
							"attributes": [
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
								}
							],
							"droppedAttributesCount": 1,
							"flags": 1,
							"traceId": "",
							"spanId": ""
						},
						{
							"timeUnixNano": "1669112524002",
							"observedTimeUnixNano": "1669112524002",
							"severityNumber": 17,
							"severityText": "Error",
							"body": {
								"stringValue": "{\"msg\":\"something else happened\"}"
							},
							"attributes": [
								{
									"key": "kusto.table",
									"value": {
										"stringValue": "CTable"
									}
								},
								{
									"key": "kusto.database",
									"value": {
										"stringValue": "BDatabase"
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
