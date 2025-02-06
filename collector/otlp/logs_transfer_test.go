package otlp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	commonv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	logsv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/logs/v1"
	resourcev1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/resource/v1"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/storage"
	"github.com/golang/protobuf/proto"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestLogsServiceE2E(t *testing.T) {
	t.Run("rawlog_2_dbs", func(t *testing.T) {
		var msg v1.ExportLogsServiceRequest
		require.NoError(t, protojson.Unmarshal(rawlog, &msg))

		resp, store := makeRequest(t, &msg)
		require.Equal(t, http.StatusOK, resp.Code)
		require.Equal(t, "application/x-protobuf", resp.Header().Get("Content-Type"))

		var exportLogsResponse v1.ExportLogsServiceResponse
		err := proto.Unmarshal(resp.Body.Bytes(), &exportLogsResponse)
		require.NoError(t, err)
		require.Nil(t, exportLogsResponse.GetPartialSuccess())

		require.NoError(t, store.Close())
		keys := store.PrefixesByAge()
		require.Equal(t, 2, len(keys))
		require.True(t, strings.HasPrefix(string(keys[0]), "ADatabase_ATable_"))
		require.True(t, strings.HasPrefix(string(keys[1]), "BDatabase_BTable_"))
	})

	t.Run("log_without_routing_info", func(t *testing.T) {
		msg := exportLogsRequest([]*logsv1.LogRecord{
			{ // no routing info in the body or in attributes
				TimeUnixNano:         1,
				ObservedTimeUnixNano: 1,
				Body: &commonv1.AnyValue{
					Value: &commonv1.AnyValue_StringValue{StringValue: "body"},
				},
			},
			{ // valid routing info in the body
				TimeUnixNano:         1,
				ObservedTimeUnixNano: 1,
				Body: &commonv1.AnyValue{
					Value: &commonv1.AnyValue_KvlistValue{
						KvlistValue: &commonv1.KeyValueList{
							Values: []*commonv1.KeyValue{
								{
									Key: "message",
									Value: &commonv1.AnyValue{
										Value: &commonv1.AnyValue_StringValue{StringValue: "message"},
									},
								},
								{
									Key: "kusto.database",
									Value: &commonv1.AnyValue{
										Value: &commonv1.AnyValue_StringValue{StringValue: "ADatabase"},
									},
								},
								{
									Key: "kusto.table",
									Value: &commonv1.AnyValue{
										Value: &commonv1.AnyValue_StringValue{StringValue: "BTable"},
									},
								},
							},
						},
					},
				},
			},
			{ // valid routing info in attributes
				Attributes: []*commonv1.KeyValue{
					{
						Key: "kusto.database",
						Value: &commonv1.AnyValue{
							Value: &commonv1.AnyValue_StringValue{StringValue: "BDatabase"},
						},
					},
					{
						Key: "kusto.table",
						Value: &commonv1.AnyValue{
							Value: &commonv1.AnyValue_StringValue{StringValue: "CTable"},
						},
					},
				},
				TimeUnixNano:         1,
				ObservedTimeUnixNano: 1,
				Body: &commonv1.AnyValue{
					Value: &commonv1.AnyValue_KvlistValue{
						KvlistValue: &commonv1.KeyValueList{
							Values: []*commonv1.KeyValue{
								{
									Key: "message",
									Value: &commonv1.AnyValue{
										Value: &commonv1.AnyValue_StringValue{StringValue: "message"},
									},
								},
							},
						},
					},
				},
			},
		})

		resp, store := makeRequest(t, msg)
		require.Equal(t, http.StatusOK, resp.Code)
		require.Equal(t, "application/x-protobuf", resp.Header().Get("Content-Type"))

		var exportLogsResponse v1.ExportLogsServiceResponse
		err := proto.Unmarshal(resp.Body.Bytes(), &exportLogsResponse)
		require.NoError(t, err)
		require.NotNil(t, exportLogsResponse.GetPartialSuccess())
		require.Equal(t, int64(1), exportLogsResponse.GetPartialSuccess().GetRejectedLogRecords())

		require.NoError(t, store.Close())
		keys := store.PrefixesByAge()
		require.Equal(t, 2, len(keys))
		require.True(t, strings.HasPrefix(string(keys[0]), "ADatabase_BTable_"))
		require.True(t, strings.HasPrefix(string(keys[1]), "BDatabase_CTable_"))
	})
}

func TestLogsService_Overloaded(t *testing.T) {
	dir := t.TempDir()

	store := storage.NewLocalStore(
		storage.StoreOpts{
			StorageDir: dir,
		})

	require.NoError(t, store.Open(context.Background()))
	defer store.Close()
	s := NewLogsService(LogsServiceOpts{
		Store:         store,
		HealthChecker: fakeHealthChecker{false},
	})
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	var msg v1.ExportLogsServiceRequest
	require.NoError(t, protojson.Unmarshal(rawlog, &msg))

	b, err := proto.Marshal(&msg)
	require.NoError(t, err)

	req, err := http.NewRequest("POST", "/v1/logs", bytes.NewReader(b))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-protobuf")

	resp := httptest.NewRecorder()
	s.Handler(resp, req)
	require.Equal(t, http.StatusTooManyRequests, resp.Code)

	require.NoError(t, store.Close())

	keys := store.PrefixesByAge()
	require.Equal(t, 0, len(keys))
}

func BenchmarkLogsService(b *testing.B) {
	dir := b.TempDir()

	store := storage.NewLocalStore(
		storage.StoreOpts{
			StorageDir: dir,
		})

	require.NoError(b, store.Open(context.Background()))
	defer store.Close()
	s := NewLogsService(LogsServiceOpts{
		Store:         store,
		HealthChecker: fakeHealthChecker{true},
	})
	require.NoError(b, s.Open(context.Background()))
	defer s.Close()

	var msg v1.ExportLogsServiceRequest
	require.NoError(b, protojson.Unmarshal(rawlog, &msg))

	serialized, err := proto.Marshal(&msg)
	require.NoError(b, err)

	recorder := httptest.NewRecorder()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("POST", "/v1/logs", bytes.NewReader(serialized))
		req.Header.Set("Content-Type", "application/x-protobuf")

		s.Handler(recorder, req)
	}
}

func TestConvertToLogBatch(t *testing.T) {
	getLogbatch := func() *types.LogBatch {
		logBatch := types.LogBatchPool.Get(1).(*types.LogBatch)
		logBatch.Reset()
		return logBatch
	}

	tests := []struct {
		name                string
		req                 *v1.ExportLogsServiceRequest
		expectedFunc        func() *types.LogBatch
		expectedDroppedLogs int64
	}{
		{
			name:                "nil request",
			req:                 nil,
			expectedFunc:        getLogbatch,
			expectedDroppedLogs: 0,
		},
		{
			name:                "empty request",
			req:                 &v1.ExportLogsServiceRequest{},
			expectedFunc:        getLogbatch,
			expectedDroppedLogs: 0,
		},
		{
			name: "nil resource log entry",
			req: &v1.ExportLogsServiceRequest{
				ResourceLogs: []*logsv1.ResourceLogs{
					nil,
					{
						// empty
					},
				},
			},
			expectedFunc:        getLogbatch,
			expectedDroppedLogs: 0,
		},
		{
			name: "nil scope logs",
			req: &v1.ExportLogsServiceRequest{
				ResourceLogs: []*logsv1.ResourceLogs{
					{
						ScopeLogs: []*logsv1.ScopeLogs{
							nil,
							{
								// empty
							},
						},
					},
				},
			},
			expectedFunc:        getLogbatch,
			expectedDroppedLogs: 0,
		},
		{
			name: "nil log records",
			req: &v1.ExportLogsServiceRequest{
				ResourceLogs: []*logsv1.ResourceLogs{
					{
						ScopeLogs: []*logsv1.ScopeLogs{
							{
								LogRecords: []*logsv1.LogRecord{
									nil,
								},
							},
						},
					},
				},
			},
			expectedFunc:        getLogbatch,
			expectedDroppedLogs: 0,
		},
		{
			name:                "log without routing info",
			expectedDroppedLogs: 1,
			expectedFunc: func() *types.LogBatch {
				batch := getLogbatch()
				log := types.LogPool.Get(1).(*types.Log)
				log.Reset()

				log.Timestamp = 1
				log.ObservedTimestamp = 2
				log.Body = map[string]any{
					"message":        "message",
					"kusto.database": "ADatabase",
					"kusto.table":    "BTable",
				}
				log.Attributes = map[string]any{
					"adxmon_destination_database": "ADatabase",
					"adxmon_destination_table":    "BTable",
				}

				batch.Logs = append(batch.Logs, log)
				return batch
			},
			req: &v1.ExportLogsServiceRequest{
				ResourceLogs: []*logsv1.ResourceLogs{
					nil,
					{
						Resource: &resourcev1.Resource{},
						ScopeLogs: []*logsv1.ScopeLogs{
							nil,
							{
								LogRecords: []*logsv1.LogRecord{
									nil,
									{ // no routing info in the body or in attributes
										TimeUnixNano:         1,
										ObservedTimeUnixNano: 2,
										Body: &commonv1.AnyValue{
											Value: &commonv1.AnyValue_StringValue{StringValue: "body"},
										},
									},
									{ // valid routing info in the body
										TimeUnixNano:         1,
										ObservedTimeUnixNano: 2,
										Body: &commonv1.AnyValue{
											Value: &commonv1.AnyValue_KvlistValue{
												KvlistValue: &commonv1.KeyValueList{
													Values: []*commonv1.KeyValue{
														{
															Key: "message",
															Value: &commonv1.AnyValue{
																Value: &commonv1.AnyValue_StringValue{StringValue: "message"},
															},
														},
														{
															Key: "kusto.database",
															Value: &commonv1.AnyValue{
																Value: &commonv1.AnyValue_StringValue{StringValue: "ADatabase"},
															},
														},
														{
															Key: "kusto.table",
															Value: &commonv1.AnyValue{
																Value: &commonv1.AnyValue_StringValue{StringValue: "BTable"},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:                "log with resource",
			expectedDroppedLogs: 0,
			expectedFunc: func() *types.LogBatch {
				batch := getLogbatch()
				log := types.LogPool.Get(1).(*types.Log)
				log.Reset()

				log.Timestamp = 1
				log.ObservedTimestamp = 2
				log.Body = map[string]any{
					"message":        "message",
					"kusto.database": "ADatabase",
					"kusto.table":    "BTable",
				}
				log.Attributes = map[string]any{
					"adxmon_destination_database": "ADatabase",
					"adxmon_destination_table":    "BTable",
				}
				log.Resource = map[string]any{
					"hello": "world",
				}

				batch.Logs = append(batch.Logs, log)
				return batch
			},
			req: &v1.ExportLogsServiceRequest{
				ResourceLogs: []*logsv1.ResourceLogs{
					nil,
					{
						Resource: &resourcev1.Resource{
							Attributes: []*commonv1.KeyValue{
								nil,
								{
									Key: "hello",
									Value: &commonv1.AnyValue{
										Value: &commonv1.AnyValue_StringValue{StringValue: "world"},
									},
								},
							},
						},
						ScopeLogs: []*logsv1.ScopeLogs{
							nil,
							{
								LogRecords: []*logsv1.LogRecord{
									nil,
									{ // valid routing info in the body
										TimeUnixNano:         1,
										ObservedTimeUnixNano: 2,
										Body: &commonv1.AnyValue{
											Value: &commonv1.AnyValue_KvlistValue{
												KvlistValue: &commonv1.KeyValueList{
													Values: []*commonv1.KeyValue{
														{
															Key: "message",
															Value: &commonv1.AnyValue{
																Value: &commonv1.AnyValue_StringValue{StringValue: "message"},
															},
														},
														{
															Key: "kusto.database",
															Value: &commonv1.AnyValue{
																Value: &commonv1.AnyValue_StringValue{StringValue: "ADatabase"},
															},
														},
														{
															Key: "kusto.table",
															Value: &commonv1.AnyValue{
																Value: &commonv1.AnyValue_StringValue{StringValue: "BTable"},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			store := storage.NewLocalStore(
				storage.StoreOpts{
					StorageDir: dir,
				})

			require.NoError(t, store.Open(context.Background()))
			defer store.Close()
			s := NewLogsService(LogsServiceOpts{
				Store:         store,
				HealthChecker: fakeHealthChecker{false},
			})
			logBatch := types.LogBatchPool.Get(1).(*types.LogBatch)
			logBatch.Reset()
			droppedLogs := s.convertToLogBatch(tt.req, logBatch)

			expectedLogBatch := tt.expectedFunc()
			require.Equal(t, tt.expectedDroppedLogs, droppedLogs)
			require.ElementsMatch(t, expectedLogBatch.Logs, logBatch.Logs)
		})
	}
}

func TestExtractBody(t *testing.T) {
	tests := []struct {
		name     string
		body     *commonv1.AnyValue
		expected map[string]any
	}{
		{
			name:     "nil body",
			body:     nil,
			expected: map[string]any{},
		},
		{
			name:     "empty body",
			body:     &commonv1.AnyValue{},
			expected: map[string]any{},
		},
		{
			name: "string value",
			body: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_StringValue{StringValue: "test"},
			},
			expected: map[string]any{
				types.BodyKeyMessage: "test",
			},
		},
		{
			name: "int value",
			body: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_IntValue{IntValue: 123},
			},
			expected: map[string]any{
				types.BodyKeyMessage: int64(123),
			},
		},
		{
			name: "array value",
			body: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_ArrayValue{
					ArrayValue: &commonv1.ArrayValue{
						Values: []*commonv1.AnyValue{
							{
								Value: &commonv1.AnyValue_StringValue{StringValue: "value1"},
							},
							{
								Value: &commonv1.AnyValue_IntValue{IntValue: 1},
							},
						},
					},
				},
			},
			expected: map[string]any{
				types.BodyKeyMessage: []any{"value1", int64(1)},
			},
		},
		{
			name: "kvlist value with no value",
			body: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_KvlistValue{
					KvlistValue: nil,
				},
			},
			expected: map[string]any{},
		},
		{
			name: "kvlist value",
			body: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_KvlistValue{
					KvlistValue: &commonv1.KeyValueList{
						Values: []*commonv1.KeyValue{
							{
								Key: "key1",
								Value: &commonv1.AnyValue{
									Value: &commonv1.AnyValue_StringValue{StringValue: "value1"},
								},
							},
							{
								Key: "key2",
								Value: &commonv1.AnyValue{
									Value: &commonv1.AnyValue_IntValue{IntValue: 123},
								},
							},
						},
					},
				},
			},
			expected: map[string]any{
				"key1": "value1",
				"key2": int64(123),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := make(map[string]any)
			extractBody(tt.body, dest)
			require.Equal(t, tt.expected, dest)
		})
	}
}

func TestExtractKeyValues(t *testing.T) {
	tests := []struct {
		name     string
		attrs    []*commonv1.KeyValue
		expected map[string]any
	}{
		{
			name:     "nil attributes",
			attrs:    nil,
			expected: map[string]any{},
		},
		{
			name:     "empty attributes",
			attrs:    []*commonv1.KeyValue{},
			expected: map[string]any{},
		},
		{
			name: "empty elements",
			attrs: []*commonv1.KeyValue{
				{
					Key: "emptyvalue",
				},
				{},
				nil,
				{
					Key: "key1",
					Value: &commonv1.AnyValue{
						Value: &commonv1.AnyValue_StringValue{StringValue: "value1"},
					},
				},
			},
			expected: map[string]any{
				"key1": "value1",
			},
		},
		{
			name: "string attribute",
			attrs: []*commonv1.KeyValue{
				{
					Key: "key1",
					Value: &commonv1.AnyValue{
						Value: &commonv1.AnyValue_StringValue{StringValue: "value1"},
					},
				},
			},
			expected: map[string]any{
				"key1": "value1",
			},
		},
		{
			name: "int attribute",
			attrs: []*commonv1.KeyValue{
				{
					Key: "key1",
					Value: &commonv1.AnyValue{
						Value: &commonv1.AnyValue_IntValue{IntValue: 123},
					},
				},
			},
			expected: map[string]any{
				"key1": int64(123),
			},
		},
		{
			name: "nested attribute",
			attrs: []*commonv1.KeyValue{
				{
					Key: "key1",
					Value: &commonv1.AnyValue{
						Value: &commonv1.AnyValue_KvlistValue{
							KvlistValue: &commonv1.KeyValueList{
								Values: []*commonv1.KeyValue{
									{
										Key: "nestedkey1",
										Value: &commonv1.AnyValue{
											Value: &commonv1.AnyValue_StringValue{StringValue: "nestedvalue1"},
										},
									},
								},
							},
						},
					},
				},
				{
					Key: "key2",
					Value: &commonv1.AnyValue{
						Value: &commonv1.AnyValue_IntValue{IntValue: 123},
					},
				},
			},
			expected: map[string]any{
				"key1": map[string]any{
					"nestedkey1": "nestedvalue1",
				},
				"key2": int64(123),
			},
		},
		{
			name: "multiple attributes",
			attrs: []*commonv1.KeyValue{
				{
					Key: "key1",
					Value: &commonv1.AnyValue{
						Value: &commonv1.AnyValue_StringValue{StringValue: "value1"},
					},
				},
				{
					Key: "key2",
					Value: &commonv1.AnyValue{
						Value: &commonv1.AnyValue_IntValue{IntValue: 123},
					},
				},
			},
			expected: map[string]any{
				"key1": "value1",
				"key2": int64(123),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := make(map[string]any)
			extractKeyValues(tt.attrs, dest)
			require.Equal(t, tt.expected, dest)
		})
	}
}

func TestExtract(t *testing.T) {
	tests := []struct {
		name       string
		anyValue   *commonv1.AnyValue
		expected   any
		expectedOk bool
		maxDepth   int
	}{
		{
			name:       "nil value",
			anyValue:   nil,
			expectedOk: false,
		},
		{
			name:       "empty value",
			anyValue:   &commonv1.AnyValue{},
			expectedOk: false,
		},
		{
			name: "string value",
			anyValue: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_StringValue{StringValue: "test"},
			},
			expected:   "test",
			expectedOk: true,
		},
		{
			name: "int value",
			anyValue: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_IntValue{IntValue: 123},
			},
			expected:   int64(123),
			expectedOk: true,
		},
		{
			name: "double value",
			anyValue: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_DoubleValue{DoubleValue: 123.45},
			},
			expected:   123.45,
			expectedOk: true,
		},
		{
			name: "bool value",
			anyValue: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_BoolValue{BoolValue: true},
			},
			expected:   true,
			expectedOk: true,
		},
		{
			name: "bytes value",
			anyValue: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_BytesValue{BytesValue: []byte("test")},
			},
			expected:   []byte("test"),
			expectedOk: true,
		},
		{
			name: "array value",
			anyValue: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_ArrayValue{
					ArrayValue: &commonv1.ArrayValue{
						Values: []*commonv1.AnyValue{
							{
								Value: &commonv1.AnyValue_StringValue{StringValue: "value1"},
							},
							{
								Value: &commonv1.AnyValue_IntValue{IntValue: 1},
							},
						},
					},
				},
			},
			expected:   []any{"value1", int64(1)},
			expectedOk: true,
		},
		{
			name: "array value with busted values",
			anyValue: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_ArrayValue{
					ArrayValue: &commonv1.ArrayValue{
						Values: []*commonv1.AnyValue{
							nil,
							{},
							{
								Value: &commonv1.AnyValue_KvlistValue{
									KvlistValue: nil,
								},
							},
							{
								Value: &commonv1.AnyValue_ArrayValue{
									ArrayValue: nil,
								},
							},
							{
								Value: &commonv1.AnyValue_StringValue{StringValue: "value1"},
							},
							{
								Value: &commonv1.AnyValue_IntValue{IntValue: 1},
							},
						},
					},
				},
			},
			expected:   []any{"value1", int64(1)},
			expectedOk: true,
		},
		{
			name: "kvlist value",
			anyValue: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_KvlistValue{
					KvlistValue: &commonv1.KeyValueList{
						Values: []*commonv1.KeyValue{
							{
								Key: "key1",
								Value: &commonv1.AnyValue{
									Value: &commonv1.AnyValue_StringValue{StringValue: "value1"},
								},
							},
							{
								Key: "key2",
								Value: &commonv1.AnyValue{
									Value: &commonv1.AnyValue_IntValue{IntValue: 123},
								},
							},
						},
					},
				},
			},
			expected: map[string]any{
				"key1": "value1",
				"key2": int64(123),
			},
			expectedOk: true,
		},
		{
			name: "kvlist value with busted values",
			anyValue: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_KvlistValue{
					KvlistValue: &commonv1.KeyValueList{
						Values: []*commonv1.KeyValue{
							{
								Key: "key1",
								Value: &commonv1.AnyValue{
									Value: &commonv1.AnyValue_StringValue{StringValue: "value1"},
								},
							},
							nil,
							{},
							{
								Key: "busted",
								Value: &commonv1.AnyValue{
									Value: nil,
								},
							},
							{
								Key: "busted2",
								Value: &commonv1.AnyValue{
									Value: &commonv1.AnyValue_KvlistValue{
										KvlistValue: nil,
									},
								},
							},
							{
								Key: "busted3",
								Value: &commonv1.AnyValue{
									Value: &commonv1.AnyValue_KvlistValue{
										KvlistValue: &commonv1.KeyValueList{
											Values: []*commonv1.KeyValue{
												{
													Key: "busted3subkey",
													Value: &commonv1.AnyValue{
														Value: nil,
													},
												},
											},
										},
									},
								},
							},
							{
								Key: "key2",
								Value: &commonv1.AnyValue{
									Value: &commonv1.AnyValue_IntValue{IntValue: 123},
								},
							},
						},
					},
				},
			},
			expected: map[string]any{
				"key1":    "value1",
				"key2":    int64(123),
				"busted3": map[string]any{}, // top level ok, all subkeys broken
			},
			expectedOk: true,
		},
		{
			name: "nested kvlist value",
			anyValue: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_KvlistValue{
					KvlistValue: &commonv1.KeyValueList{
						Values: []*commonv1.KeyValue{
							{
								Key: "key1",
								Value: &commonv1.AnyValue{
									Value: &commonv1.AnyValue_KvlistValue{
										KvlistValue: &commonv1.KeyValueList{
											Values: []*commonv1.KeyValue{
												{
													Key: "nestedkey1",
													Value: &commonv1.AnyValue{
														Value: &commonv1.AnyValue_StringValue{StringValue: "nestedvalue1"},
													},
												},
											},
										},
									},
								},
							},
							{
								Key: "key2",
								Value: &commonv1.AnyValue{
									Value: &commonv1.AnyValue_IntValue{IntValue: 123},
								},
							},
						},
					},
				},
			},
			expected: map[string]any{
				"key1": map[string]any{
					"nestedkey1": "nestedvalue1",
				},
				"key2": int64(123),
			},
			expectedOk: true,
		},
		{
			name: "nested kvlist value beyond max depth",
			anyValue: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_KvlistValue{
					KvlistValue: &commonv1.KeyValueList{
						Values: []*commonv1.KeyValue{
							{
								Key: "key1",
								Value: &commonv1.AnyValue{
									Value: &commonv1.AnyValue_KvlistValue{
										KvlistValue: &commonv1.KeyValueList{
											Values: []*commonv1.KeyValue{
												{
													Key: "nestedkey1",
													Value: &commonv1.AnyValue{
														Value: &commonv1.AnyValue_StringValue{StringValue: "nestedvalue1"},
													},
												},
											},
										},
									},
								},
							},
							{
								Key: "key2",
								Value: &commonv1.AnyValue{
									Value: &commonv1.AnyValue_IntValue{IntValue: 123},
								},
							},
						},
					},
				},
			},
			expected: map[string]any{
				"key1": map[string]any{
					"nestedkey1": "...",
				},
				"key2": int64(123),
			},
			expectedOk: true,
			maxDepth:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maxDepth := defaultMaxDepth
			if tt.maxDepth > 0 {
				maxDepth = tt.maxDepth
			}

			value, ok := extract(tt.anyValue, 0, maxDepth)
			if tt.expectedOk {
				require.True(t, ok)
				require.Equal(t, tt.expected, value)
			} else {
				require.False(t, ok)
			}
		})
	}
}

type fakeHealthChecker struct {
	healthy bool
}

func (f fakeHealthChecker) IsHealthy() bool { return f.healthy }

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
										"key": "utf8message",
										"value": {
										  "stringValue": "🔥 parse please"
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
						},
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
										  "stringValue": "something else worth logging"
										}
									  },
									  {
										"key": "utf8message",
										"value": {
										  "stringValue": "🔥 parse please"
										}
									  },
									  {
										"key": "kusto.table",
										"value": {
										  "stringValue": "BTable"
										}
									  },
									  {
										"key": "kusto.database",
										"value": {
										  "stringValue": "BDatabase"
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

func exportLogsRequest(logs []*logsv1.LogRecord) *v1.ExportLogsServiceRequest {
	return &v1.ExportLogsServiceRequest{
		ResourceLogs: []*logsv1.ResourceLogs{
			{
				ScopeLogs: []*logsv1.ScopeLogs{
					{
						LogRecords: logs,
					},
				},
			},
		},
	}
}

func makeRequest(t *testing.T, msg *v1.ExportLogsServiceRequest) (*httptest.ResponseRecorder, *storage.LocalStore) {
	t.Helper()
	dir := t.TempDir()

	store := storage.NewLocalStore(
		storage.StoreOpts{
			StorageDir: dir,
		})

	require.NoError(t, store.Open(context.Background()))
	defer store.Close()
	s := NewLogsService(LogsServiceOpts{
		Store:         store,
		HealthChecker: fakeHealthChecker{true},
	})
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	b, err := proto.Marshal(msg)
	require.NoError(t, err)

	req, err := http.NewRequest("POST", "/v1/logs", bytes.NewReader(b))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-protobuf")

	resp := httptest.NewRecorder()
	s.Handler(resp, req)
	return resp, store
}
