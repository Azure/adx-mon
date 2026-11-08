package ingestor

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/storage"
	"github.com/stretchr/testify/require"
)

// A body larger than the intake memory left is rejected as the ingestor being overloaded, so concurrent large bodies
// cannot buffer more than the intake ceiling however many connections send them.
func TestHandleTransferRejectsBodiesOverIntakeMemory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := storage.NewLocalStore(storage.StoreOpts{StorageDir: t.TempDir(), MaxDiskUsage: 1 << 30, MaxIntakeMemory: 64 << 10})
	require.NoError(t, store.Open(ctx))
	defer store.Close()
	s := &Service{store: store, health: &fakeHealthChecker{healthy: true}, databases: map[string]struct{}{"DB": {}}}

	r := httptest.NewRequest("POST", "/transfer?filename="+wal.Filename("DB", "Cpu", "", "2f2c74ffa19e6000"), bytes.NewReader(make([]byte, 1<<20)))
	resp := httptest.NewRecorder()
	s.HandleTransfer(resp, r)
	require.Equal(t, http.StatusTooManyRequests, resp.Code)
	require.Equal(t, "instance", resp.Header().Get(cluster.OverloadScopeHeader))
	require.NotEmpty(t, resp.Header().Get("Retry-After"))
}
