package otlp

import (
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Azure/adx-mon/collector/logs/engine"
	"github.com/Azure/adx-mon/collector/logs/sinks"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/storage"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func newLogsServer(t *testing.T) *httptest.Server {
	t.Helper()
	store := storage.NewLocalStore(storage.StoreOpts{StorageDir: t.TempDir(), MaxDiskUsage: 1024 * 1024})
	require.NoError(t, store.Open(context.Background()))
	t.Cleanup(func() { store.Close() })
	sink, err := sinks.NewStoreSink(sinks.StoreSinkConfig{Store: store})
	require.NoError(t, err)
	s := NewLogsService(LogsServiceOpts{
		WorkerCreator: engine.WorkerCreator(nil, []types.Sink{sink}),
		HealthChecker: fakeHealthChecker{true},
	})
	require.NoError(t, s.Open(context.Background()))
	t.Cleanup(func() { s.Close() })
	srv := httptest.NewServer(http.HandlerFunc(s.Handler))
	t.Cleanup(srv.Close)
	return srv
}

func exportBody(t *testing.T) []byte {
	t.Helper()
	var msg v1.ExportLogsServiceRequest
	require.NoError(t, protojson.Unmarshal(rawlog, &msg))
	b, err := proto.Marshal(&msg)
	require.NoError(t, err)
	return b
}

// A request without a Content-Length, sent with chunked encoding, is accepted.
func TestLogsService_AcceptsChunkedBody(t *testing.T) {
	srv := newLogsServer(t)
	// io.MultiReader hides the length, so the client sends the body chunked.
	resp, err := http.Post(srv.URL, "application/x-protobuf", io.MultiReader(bytes.NewReader(exportBody(t))))
	if err != nil {
		t.Logf("chunked request failed: %v", err)
	}
	require.NoError(t, err)
	defer resp.Body.Close()
	t.Logf("chunked request returned %s", resp.Status)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// A body larger than the limit is rejected rather than allocated.
func TestLogsService_RejectsOversizedBody(t *testing.T) {
	defer func(n int64) { maxLogsBodyBytes = n }(maxLogsBodyBytes)
	maxLogsBodyBytes = 64

	srv := newLogsServer(t)
	resp, err := http.Post(srv.URL, "application/x-protobuf", bytes.NewReader(exportBody(t)))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
}
