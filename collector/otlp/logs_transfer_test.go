package otlp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	"github.com/Azure/adx-mon/storage"
	"github.com/golang/protobuf/proto"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestLogsService(t *testing.T) {
	dir := t.TempDir()

	store := storage.NewLocalStore(
		storage.StoreOpts{
			StorageDir: dir,
		})

	require.NoError(t, store.Open(context.Background()))
	defer store.Close()
	s := NewLogsService(LogsServiceOpts{
		Store: store,
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
	require.Equal(t, http.StatusOK, resp.Code)

	require.NoError(t, store.Close())

	keys := store.PrefixesByAge()
	require.Equal(t, 1, len(keys))
	require.Equal(t, "ADatabase_ATable", string(keys[0]))
}
