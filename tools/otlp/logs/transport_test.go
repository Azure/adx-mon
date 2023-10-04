package logs_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/logs/v1/logsv1connect"
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	collectorotlp "github.com/Azure/adx-mon/collector/otlp"
	ingestorsvc "github.com/Azure/adx-mon/ingestor"
	"github.com/Azure/adx-mon/ingestor/adx"
	ingestorotlp "github.com/Azure/adx-mon/ingestor/otlp"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/pkg/otlp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func BenchmarkProxyTransport(b *testing.B) {
	b.ReportAllocs()
	ctx, cancel := context.WithCancel(context.Background())
	ingestorURL := ingestor(b, ctx)
	collectorURL, hc := collector(b, ctx, []string{ingestorURL})

	var log v1.ExportLogsServiceRequest
	err := protojson.Unmarshal(rawlog, &log)
	require.NoError(b, err)

	buf, err := proto.Marshal(&log)
	require.NoError(b, err)
	b.Logf("marshalled %d bytes", len(buf))

	u, err := url.JoinPath(collectorURL, logsv1connect.LogsServiceExportProcedure)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := hc.Post(u, "application/x-protobuf", bytes.NewReader(buf))
		require.NoError(b, err)
		require.Equal(b, resp.StatusCode, http.StatusOK)
	}

	cancel()
	// 1789	    609463 ns/op	   62762 B/op	     626 allocs/op
}

func BenchmarkTransferTransport(b *testing.B) {
	b.ReportAllocs()
	ctx, cancel := context.WithCancel(context.Background())
	ingestorURL := ingestor(b, ctx)
	collectorURL, hc := collector(b, ctx, []string{ingestorURL})

	var log v1.ExportLogsServiceRequest
	err := protojson.Unmarshal(rawlog, &log)
	require.NoError(b, err)

	buf, err := proto.Marshal(&log)
	require.NoError(b, err)
	b.Logf("marshalled %d bytes", len(buf))

	u, err := url.JoinPath(collectorURL, "/v1/logs")
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := hc.Post(u, "application/x-protobuf", bytes.NewReader(buf))
		require.NoError(b, err)
		require.Equal(b, resp.StatusCode, http.StatusOK)
	}

	cancel()
	// 20	  58973292 ns/op	 3392054 B/op	     844 allocs/op
}

func collector(b *testing.B, ctx context.Context, endpoints []string) (string, *http.Client) {
	b.Helper()
	var (
		insecureSkipVerify = true
		addAttributes      = map[string]string{
			"some-key":       "some-value",
			"some-other-key": "some-other-value",
		}
		liftAttributes = []string{"kusto.table", "kusto.database"}
	)
	mux := http.NewServeMux()
	hp := collectorotlp.LogsProxyHandler(ctx, endpoints, insecureSkipVerify, addAttributes, liftAttributes)
	ht := collectorotlp.LogsTransferHandler(ctx, endpoints, insecureSkipVerify, addAttributes, b.TempDir())
	mux.Handle(logsv1connect.LogsServiceExportProcedure, hp)
	mux.Handle("/v1/logs", ht)

	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()

	go func(srv *httptest.Server, b *testing.B) {
		b.Helper()
		<-ctx.Done()
		srv.Close()
	}(srv, b)

	return srv.URL, srv.Client()
}

func ingestor(b *testing.B, ctx context.Context) string {
	b.Helper()

	store := storage.NewLocalStore(storage.StoreOpts{
		StorageDir: b.TempDir(),
	})
	err := store.Open(ctx)
	require.NoError(b, err)

	writer := func(ctx context.Context, database, table string, logs *otlp.Logs) error {
		return store.WriteOTLPLogs(ctx, database, table, logs)
	}

	svc, err := ingestorsvc.NewService(ingestorsvc.ServiceOpts{
		StorageDir:      b.TempDir(),
		Uploader:        adx.NewFakeUploader(),
		MaxSegmentCount: int64(b.N) + 1,
		MaxDiskUsage:    10 * 1024 * 1024 * 1024,
	})
	require.NoError(b, err)

	mux := http.NewServeMux()
	mux.Handle(logsv1connect.NewLogsServiceHandler(ingestorotlp.NewLogsServer(writer, []string{"ADatabase", "BDatabase"})))
	mux.HandleFunc("/transfer", svc.HandleTransfer)

	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()

	go func(srv *httptest.Server, b *testing.B) {
		b.Helper()
		<-ctx.Done()
		srv.Close()
		err := store.Close()
		require.NoError(b, err)
	}(srv, b)

	return srv.URL
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
					},
					{
						"key": "collector",
						"value": {
							"stringValue": "some-collector"
						}
					},
					{
						"key": "environment",
						"value": {
							"stringValue": "some-env"
						}
					},
					{
						"key": "os",
						"value": {
							"stringValue": "some-os-version"
						}
					}
				]
			},
			"scopeLogs": [
				{
					"logRecords": [
						{
							"timeUnixNano": "1669112524001",
							"observedTimeUnixNano": "1669112524001",
							"severityNumber": 17,
							"severityText": "Error",
							"body": {
								"stringValue": "{ \"_id\": \"651179e7eb3591c7d1564db7\", \"index\": 0, \"guid\": \"9ecf5430-f696-417c-9f24-4f831e66e024\", \"isActive\": true, \"age\": 32, \"eyeColor\": \"green\", \"name\": \"Sandra Owen\", \"company\": \"BIOTICA\", \"email\": \"sandraowen@biotica.com\", \"phone\": \"+1 (939) 546-3845\", \"address\": \"317 Melba Court, Colton, Montana, 1596\", \"about\": \"Mollit officia excepteur velit ut magna nisi voluptate ut exercitation cupidatat laborum ad deserunt. Incididunt incididunt voluptate fugiat enim. In aliquip laboris excepteur nulla sint veniam pariatur in. Esse est exercitation dolore qui quis proident amet ut eiusmod anim adipisicing.\r\n\", \"registered\": \"2015-06-18T12:26:45 +06:00\", \"latitude\": 50.041325, \"longitude\": -29.437762, \"tags\": [ \"nisi\", \"quis\", \"magna\", \"id\", \"do\", \"ut\", \"amet\" ], \"friends\": [ { \"id\": 0, \"name\": \"Boyer Hensley\" }, { \"id\": 1, \"name\": \"Harriett Nguyen\" }, { \"id\": 2, \"name\": \"Beatriz Stevenson\" } ], \"greeting\": \"Hello, Sandra Owen! You have 8 unread messages.\", \"favoriteFruit\": \"apple\" }"
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
							"flags": 1,
							"traceId": "",
							"spanId": ""
						},
						{
							"timeUnixNano": "1669112525001",
							"observedTimeUnixNano": "1669112525001",
							"severityNumber": 17,
							"severityText": "Error",
							"body": {
								"stringValue": "{ \"_id\": \"651179e7a24457da4d2bbfa2\", \"index\": 1, \"guid\": \"f13ecc0e-dfc1-4237-aa61-7ba6957cc100\", \"isActive\": true, \"age\": 33, \"eyeColor\": \"green\", \"name\": \"Stacey Odom\", \"company\": \"BIFLEX\", \"email\": \"staceyodom@biflex.com\", \"phone\": \"+1 (916) 543-3574\", \"address\": \"382 Gerry Street, Winchester, South Carolina, 4292\", \"about\": \"Aliquip in excepteur ipsum eiusmod qui non commodo elit consectetur nostrud. Reprehenderit reprehenderit culpa esse veniam officia nostrud reprehenderit excepteur nostrud aliquip magna do nulla esse. Labore nostrud ad qui irure fugiat. Ipsum velit laboris ad esse deserunt labore veniam. Et ad consequat elit incididunt Lorem aute proident qui eu.\r\n\", \"registered\": \"2015-08-25T05:04:02 +06:00\", \"latitude\": -49.915028, \"longitude\": -163.986714, \"tags\": [ \"incididunt\", \"proident\", \"pariatur\", \"eu\", \"duis\", \"cupidatat\", \"ipsum\" ], \"friends\": [ { \"id\": 0, \"name\": \"Frazier Pennington\" }, { \"id\": 1, \"name\": \"Duncan Terry\" }, { \"id\": 2, \"name\": \"Helene Brooks\" } ], \"greeting\": \"Hello, Stacey Odom! You have 9 unread messages.\", \"favoriteFruit\": \"banana\" }"
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
							"flags": 1,
							"traceId": "",
							"spanId": ""
						},
						{
							"timeUnixNano": "1669112525021",
							"observedTimeUnixNano": "1669112525021",
							"severityNumber": 17,
							"severityText": "Error",
							"body": {
								"stringValue": "{ \"_id\": \"651179e70ed89fffbca51d5f\", \"index\": 2, \"guid\": \"e4c4d493-f264-4557-b269-50af34bd023a\", \"isActive\": false, \"age\": 22, \"eyeColor\": \"brown\", \"name\": \"Fernandez Robinson\", \"company\": \"KNEEDLES\", \"email\": \"fernandezrobinson@kneedles.com\", \"phone\": \"+1 (884) 471-3434\", \"address\": \"734 Florence Avenue, Tilleda, Tennessee, 4019\", \"about\": \"Labore esse ullamco anim excepteur Lorem. Laboris irure cillum officia enim. Sit enim in sit enim Lorem eiusmod. Excepteur ipsum exercitation aute fugiat eiusmod laboris.\r\n\", \"registered\": \"2022-12-12T04:57:33 +07:00\", \"latitude\": -7.582694, \"longitude\": 146.669878, \"tags\": [ \"elit\", \"magna\", \"Lorem\", \"sint\", \"do\", \"in\", \"exercitation\" ], \"friends\": [ { \"id\": 0, \"name\": \"Johns Hyde\" }, { \"id\": 1, \"name\": \"Luna Valdez\" }, { \"id\": 2, \"name\": \"Bettye Brady\" } ], \"greeting\": \"Hello, Fernandez Robinson! You have 9 unread messages.\", \"favoriteFruit\": \"apple\" }"
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
