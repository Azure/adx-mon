package otlp

import (
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	"bytes"
	"context"
	"github.com/Azure/adx-mon/collector/logs/engine"
	"github.com/Azure/adx-mon/collector/logs/sinks"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/storage"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOTLPLogAdmissionBeforeResponse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := wal.NewAdmission(wal.AdmissionOpts{Enforce: true, Default: wal.Usage{Bytes: 10000, Segments: 10}, Overrides: []wal.TableLimit{{Database: "ADatabase", Table: "ATable", MaxBytes: 8, MaxSegments: 1}}})
	store := storage.NewLocalStore(storage.StoreOpts{StorageDir: t.TempDir(), Admission: a})
	require.NoError(t, store.Open(ctx))
	defer store.Close()
	sink, err := sinks.NewStoreSink(sinks.StoreSinkConfig{Store: store})
	require.NoError(t, err)
	processor := engine.WorkerCreator(nil, []types.Sink{sink})("otlp-test", nil)
	s := NewLogsService(LogsServiceOpts{HealthChecker: fakeHealthChecker{healthy: true}, ProcessBatch: processor.ProcessBatch})
	var req v1.ExportLogsServiceRequest
	require.NoError(t, protojson.Unmarshal(rawlog, &req))
	data, err := proto.Marshal(&req)
	require.NoError(t, err)
	r := httptest.NewRequest("POST", "/v1/logs", bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/x-protobuf")
	resp := httptest.NewRecorder()
	s.Handler(resp, r)
	require.Equal(t, http.StatusOK, resp.Code)
	var response v1.ExportLogsServiceResponse
	require.NoError(t, proto.Unmarshal(resp.Body.Bytes(), &response))
	require.Greater(t, response.GetPartialSuccess().GetRejectedLogRecords(), int64(0))
	require.Greater(t, a.Snapshot()[wal.Destination{Database: "BDatabase", Table: "BTable"}].Bytes, int64(8))
}
