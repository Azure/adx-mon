package cluster

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

// keyedPartitioner routes by segment key so schema prefixes of one table can be
// owned by different peers, which is what makes destination-only retry state
// over-block.
type keyedPartitioner struct {
	owners map[string]string
	addrs  map[string]string
}

func (p *keyedPartitioner) Owner(b []byte) (string, string) {
	for prefix, owner := range p.owners {
		if strings.HasPrefix(string(b), prefix) {
			return owner, p.addrs[owner]
		}
	}
	return "unknown", ""
}

// A table throttled on one peer must keep flowing to a different peer that owns
// another schema prefix of the same table.
func TestTableOverloadOnOnePeerDoesNotBlockAnother(t *testing.T) {
	var overloadedCalls, healthyCalls atomic.Int64

	overloaded := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		overloadedCalls.Add(1)
		w.Header().Set(OverloadScopeHeader, "table")
		w.Header().Set(OverloadReasonHeader, "bytes")
		w.Header().Set("Retry-After", "300")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer overloaded.Close()

	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		healthyCalls.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer healthy.Close()

	partitioner := &keyedPartitioner{
		owners: map[string]string{"DB_A_busy": "peer-busy", "DB_A_calm": "peer-calm"},
		addrs:  map[string]string{"peer-busy": overloaded.URL, "peer-calm": healthy.URL},
	}

	rep, err := NewReplicator(ReplicatorOpts{
		Partitioner:            partitioner,
		Health:                 NewHealth(HealthOpts{}),
		Hostname:               "local",
		MaxTransferConcurrency: 2,
		DisableGzip:            true,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, rep.Open(ctx))
	defer rep.Close()

	dir := t.TempDir()
	makeBatch := func(prefix string) *Batch {
		seg, err := wal.NewSegment(dir, prefix)
		require.NoError(t, err)
		_, err = seg.Write(ctx, []byte("payload"))
		require.NoError(t, err)
		require.NoError(t, seg.Close())
		return &Batch{Database: "DB", Table: "A", Prefix: prefix, Segments: []wal.SegmentInfo{seg.Info()}, batcher: &fakeBatcher{}}
	}

	busy := makeBatch("DB_A_busy")
	rep.TransferQueue() <- busy
	require.Eventually(t, busy.IsReleased, 5*time.Second, time.Millisecond)
	require.EqualValues(t, 1, overloadedCalls.Load())

	// The same table, owned by a healthy peer, must still be delivered.
	calm := makeBatch("DB_A_calm")
	rep.TransferQueue() <- calm
	require.Eventually(t, func() bool { return healthyCalls.Load() == 1 }, 5*time.Second, time.Millisecond,
		"table throttled on one peer blocked delivery to a healthy peer")
	require.Eventually(t, calm.IsRemoved, 5*time.Second, time.Millisecond)
}

// The per-peer backoff still suppresses repeat calls to the overloaded peer.
func TestOverloadedPeerIsNotRetriedImmediately(t *testing.T) {
	var calls atomic.Int64
	overloaded := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set(OverloadScopeHeader, "table")
		w.Header().Set("Retry-After", "300")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer overloaded.Close()

	rep, err := NewReplicator(ReplicatorOpts{
		Partitioner:            &fakePartitioner{owner: "peer", addr: overloaded.URL},
		Health:                 NewHealth(HealthOpts{}),
		Hostname:               "local",
		MaxTransferConcurrency: 1,
		DisableGzip:            true,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, rep.Open(ctx))
	defer rep.Close()

	dir := t.TempDir()
	for n := 0; n < 3; n++ {
		seg, err := wal.NewSegment(dir, "DB_A_s")
		require.NoError(t, err)
		_, err = seg.Write(ctx, []byte("payload"))
		require.NoError(t, err)
		require.NoError(t, seg.Close())
		rep.TransferQueue() <- &Batch{Database: "DB", Table: "A", Prefix: "DB_A_s", Segments: []wal.SegmentInfo{seg.Info()}, batcher: &fakeBatcher{}}
	}

	time.Sleep(300 * time.Millisecond)
	require.EqualValues(t, 1, calls.Load(), "the overloaded peer is contacted once, then backed off")
}
