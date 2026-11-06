package cluster

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

// Connections opened for a burst of concurrent transfers to one host are kept and reused by the next burst instead of
// being closed and reopened, which pays a new TCP and TLS handshake for each.
func TestReplicatorReusesConnectionsAcrossBursts(t *testing.T) {
	const workers = 5
	var newConns, arrived atomic.Int64
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hold each request until the whole burst is in flight so every worker needs its own connection.
		target := ((arrived.Add(1)-1)/workers + 1) * workers
		for deadline := time.Now().Add(2 * time.Second); arrived.Load() < target && time.Now().Before(deadline); {
			time.Sleep(time.Millisecond)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	s.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConns.Add(1)
		}
	}
	s.Start()
	defer s.Close()

	rep, err := NewReplicator(ReplicatorOpts{
		Partitioner:            &fakePartitioner{owner: "remote", addr: s.URL},
		Health:                 NewHealth(HealthOpts{}),
		Hostname:               "local",
		MaxTransferConcurrency: workers,
		DisableGzip:            true,
	})
	require.NoError(t, err)
	ctx := t.Context()
	require.NoError(t, rep.Open(ctx))
	defer rep.Close()

	dir := t.TempDir()
	burst := func(n int) {
		var batches []*Batch
		for i := range workers {
			table := fmt.Sprintf("Burst%dTable%d", n, i)
			seg, err := wal.NewSegment(dir, "DB_"+table)
			require.NoError(t, err)
			_, err = seg.Write(ctx, []byte("payload"))
			require.NoError(t, err)
			require.NoError(t, seg.Close())
			batch := &Batch{Database: "DB", Table: table, Segments: []wal.SegmentInfo{seg.Info()}, batcher: &fakeBatcher{}}
			batches = append(batches, batch)
			rep.TransferQueue() <- batch
		}
		for _, b := range batches {
			require.Eventually(t, b.IsRemoved, 5*time.Second, time.Millisecond)
		}
	}

	burst(1)
	require.EqualValues(t, workers, newConns.Load())

	// Let the transport return the connections to its idle pool.
	time.Sleep(100 * time.Millisecond)
	burst(2)
	t.Logf("connections opened for 2 bursts of %d transfers: %d", workers, newConns.Load())
	require.EqualValues(t, workers, newConns.Load(), "second burst should reuse the first burst's connections")
}
