package ingestor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/storage"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	m := &dto.Metric{}
	require.NoError(t, c.Write(m))
	return m.GetCounter().GetValue()
}

type staticOwner struct{ owner, addr string }

func (p staticOwner) Owner([]byte) (string, string) { return p.owner, p.addr }

// ownerPeer is an ingestor that owns every table and records the transfers it receives.
type ownerPeer struct {
	*httptest.Server
	mu       sync.Mutex
	received []string
	peer     []string
	handle   func(w http.ResponseWriter)
}

func newOwnerPeer(t *testing.T) *ownerPeer {
	p := &ownerPeer{}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		p.mu.Lock()
		p.received = append(p.received, r.URL.Query().Get("filename"))
		p.peer = append(p.peer, r.Header.Get(cluster.PeerTransferHeader))
		handle := p.handle
		p.mu.Unlock()
		if handle != nil {
			handle(w)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *ownerPeer) transfers() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.received...)
}

type forwardEnv struct {
	s      *Service
	dir    string
	owner  *ownerPeer
	health *cluster.Health
}

// newForwardEnv returns an ingestor "local" that receives collector transfers for tables owned by another ingestor.
func newForwardEnv(t *testing.T) *forwardEnv {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	dir := t.TempDir()
	store := storage.NewLocalStore(storage.StoreOpts{StorageDir: dir, MaxDiskUsage: 1 << 30})
	require.NoError(t, store.Open(ctx))
	t.Cleanup(func() { store.Close() })

	env := &forwardEnv{dir: dir, owner: newOwnerPeer(t), health: cluster.NewHealth(cluster.HealthOpts{UnhealthyTimeout: time.Minute})}
	env.s = &Service{store: store, health: &fakeHealthChecker{healthy: true}, databases: map[string]struct{}{"DB": {}}}
	forwarder, err := newTransferForwarder("local", staticOwner{owner: "owner", addr: env.owner.URL}, env.health, false)
	require.NoError(t, err)
	env.s.forwarder = forwarder
	return env
}

// transfer sends a collector transfer for table to the local ingestor.
func (e *forwardEnv) transfer(t *testing.T, table string, header http.Header) int {
	t.Helper()
	seg, err := wal.NewSegment(t.TempDir(), "DB_"+table)
	require.NoError(t, err)
	_, err = seg.Write(context.Background(), []byte("1,2,3\n"))
	require.NoError(t, err)
	require.NoError(t, seg.Close())
	payload, err := os.ReadFile(seg.Path())
	require.NoError(t, err)

	r := httptest.NewRequest("POST", "/transfer?filename="+wal.Filename("DB", table, "", "2f2c74ffa19e6000"), bytes.NewReader(payload))
	for k, v := range header {
		r.Header[k] = v
	}
	resp := httptest.NewRecorder()
	e.s.HandleTransfer(resp, r)
	return resp.Code
}

// storedLocally reports whether the local ingestor has a WAL segment for table.
func (e *forwardEnv) storedLocally(t *testing.T, table string) bool {
	t.Helper()
	entries, err := os.ReadDir(e.dir)
	require.NoError(t, err)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "DB_"+table+"_") {
			return true
		}
	}
	return false
}

// A collector transfer for a table owned by another ingestor reaches the owner before the collector's request
// returns, instead of waiting here for this node's segment to rotate and then be transferred.
func TestHandleTransferForwardsToOwner(t *testing.T) {
	env := newForwardEnv(t)
	forwarded := counterValue(t, metrics.IngestorTransferForwards.WithLabelValues("forwarded"))
	require.Equal(t, http.StatusAccepted, env.transfer(t, "Cpu", nil))
	require.Equal(t, 1.0, counterValue(t, metrics.IngestorTransferForwards.WithLabelValues("forwarded"))-forwarded)

	t.Logf("when the collector's request returned: owner received %v, stored locally=%v", env.owner.transfers(), env.storedLocally(t, "Cpu"))
	require.Equal(t, []string{wal.Filename("DB", "Cpu", "", "2f2c74ffa19e6000")}, env.owner.transfers())
	require.Equal(t, []string{"true"}, env.owner.peer, "forwarded transfers are marked so the owner does not forward them again")
	require.False(t, env.storedLocally(t, "Cpu"))
}

// Transfers from other ingestors were already routed to their owner and are stored without forwarding, which also
// stops transfers from bouncing between ingestors that disagree about ownership.
func TestHandleTransferDoesNotForwardPeerTransfers(t *testing.T) {
	env := newForwardEnv(t)
	require.Equal(t, http.StatusAccepted, env.transfer(t, "Cpu", http.Header{cluster.PeerTransferHeader: {"true"}}))
	require.Empty(t, env.owner.transfers())
	require.True(t, env.storedLocally(t, "Cpu"))
}

// Tables this node owns are stored locally.
func TestHandleTransferStoresOwnedTables(t *testing.T) {
	env := newForwardEnv(t)
	env.s.forwarder.partitioner = staticOwner{owner: "local"}
	require.Equal(t, http.StatusAccepted, env.transfer(t, "Cpu", nil))
	require.Empty(t, env.owner.transfers())
	require.True(t, env.storedLocally(t, "Cpu"))
}

// When forwarding fails the transfer is stored locally, as without forwarding, and the owner is skipped until it is
// healthy again.
func TestHandleTransferFallsBackWhenOwnerFails(t *testing.T) {
	env := newForwardEnv(t)
	env.owner.handle = func(w http.ResponseWriter) { w.WriteHeader(http.StatusInternalServerError) }

	require.Equal(t, http.StatusAccepted, env.transfer(t, "Cpu", nil))
	require.True(t, env.storedLocally(t, "Cpu"))
	require.False(t, env.health.IsPeerHealthy("owner"))

	require.Equal(t, http.StatusAccepted, env.transfer(t, "Mem", nil))
	require.True(t, env.storedLocally(t, "Mem"))
	require.Len(t, env.owner.transfers(), 1, "an unhealthy owner is not retried for every transfer")
}

// A table-scoped throttle stores the transfer locally without marking the owner unhealthy.
func TestHandleTransferFallsBackWhenTableThrottled(t *testing.T) {
	env := newForwardEnv(t)
	env.owner.handle = func(w http.ResponseWriter) {
		w.Header().Set(cluster.OverloadScopeHeader, "table")
		w.WriteHeader(http.StatusTooManyRequests)
	}
	require.Equal(t, http.StatusAccepted, env.transfer(t, "Cpu", nil))
	require.True(t, env.storedLocally(t, "Cpu"))
	require.True(t, env.health.IsPeerHealthy("owner"))
}

// A slow owner stores the transfer locally within the forward timeout, so the collector's request does not time out.
func TestHandleTransferFallsBackWhenOwnerSlow(t *testing.T) {
	defer func(d time.Duration) { forwardTimeout = d }(forwardTimeout)
	forwardTimeout = 200 * time.Millisecond
	env := newForwardEnv(t)
	release := make(chan struct{})
	defer close(release)
	env.owner.handle = func(w http.ResponseWriter) { <-release }

	start := time.Now()
	require.Equal(t, http.StatusAccepted, env.transfer(t, "Cpu", nil))
	require.Less(t, time.Since(start), 2*time.Second)
	require.True(t, env.storedLocally(t, "Cpu"))
}

// When every forwarding slot for the owner is in use the transfer is stored locally without waiting.
func TestHandleTransferFallsBackWhenForwardingBusy(t *testing.T) {
	env := newForwardEnv(t)
	slots := env.s.forwarder.peerSlots("owner")
	for i := 0; i < cap(slots); i++ {
		slots <- struct{}{}
	}
	busy := counterValue(t, metrics.IngestorTransferForwards.WithLabelValues("fallback_busy"))
	require.Equal(t, http.StatusAccepted, env.transfer(t, "Cpu", nil))
	require.Equal(t, 1.0, counterValue(t, metrics.IngestorTransferForwards.WithLabelValues("fallback_busy"))-busy)
	require.Empty(t, env.owner.transfers())
	require.True(t, env.storedLocally(t, "Cpu"))
	require.True(t, env.health.IsPeerHealthy("owner"))
}

// A response about the transfer itself, such as one that is too large or already exists, stores it locally without
// marking a healthy owner unhealthy, which would also stop peer transfers to it.
func TestHandleTransferRejectedTransferKeepsOwnerHealthy(t *testing.T) {
	for _, status := range []int{http.StatusRequestEntityTooLarge, http.StatusConflict, http.StatusBadRequest, http.StatusLocked} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			env := newForwardEnv(t)
			env.owner.handle = func(w http.ResponseWriter) { w.WriteHeader(status) }
			require.Equal(t, http.StatusAccepted, env.transfer(t, "Cpu", nil))
			require.True(t, env.storedLocally(t, "Cpu"))
			require.True(t, env.health.IsPeerHealthy("owner"))
		})
	}
}
