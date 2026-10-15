package cluster

import (
	"context"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestDestinationBackoffExpiryAndIsolation(t *testing.T) {
	var b destinationBackoff
	d := wal.Destination{Database: "DB", Table: "A"}
	now := time.Now()
	b.deferUntil("peer", d, now.Add(time.Minute))
	require.False(t, b.ready("peer", d, now))
	require.True(t, b.ready("peer", wal.Destination{Database: "DB", Table: "B"}, now))
	require.True(t, b.ready("other", d, now))
	require.True(t, b.ready("peer", d, now.Add(time.Minute)))
	require.Empty(t, b.deadlines)
}

func TestReplicatorTableBackoffKeepsPeerHealthy(t *testing.T) {
	var calls atomic.Int64
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set(OverloadScopeHeader, "table")
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(429)
	}))
	defer s.Close()
	health := NewHealth(HealthOpts{})
	rep, err := NewReplicator(ReplicatorOpts{Partitioner: &fakePartitioner{owner: "peer", addr: s.URL}, Health: health, Hostname: "local", MaxTransferConcurrency: 1, DisableGzip: true})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, rep.Open(ctx))
	defer rep.Close()
	seg, err := wal.NewSegment(t.TempDir(), "DB_A_s")
	require.NoError(t, err)
	_, err = seg.Write(ctx, []byte("data"))
	require.NoError(t, err)
	require.NoError(t, seg.Close())
	makeBatch := func(table string) *Batch {
		info := seg.Info()
		if table != "A" {
			other, err := wal.NewSegment(filepath.Dir(seg.Path()), "DB_"+table+"_s")
			require.NoError(t, err)
			require.NoError(t, other.Close())
			info = other.Info()
		}
		return &Batch{Segments: []wal.SegmentInfo{info}, Database: "DB", Table: table, batcher: &fakeBatcher{}}
	}
	first := makeBatch("A")
	rep.TransferQueue() <- first
	require.Eventually(t, first.IsReleased, time.Second, time.Millisecond)
	second := makeBatch("A")
	rep.TransferQueue() <- second
	require.EqualValues(t, 1, calls.Load())
	require.False(t, first.IsRemoved())
	require.True(t, health.IsPeerHealthy("peer"))
	other := makeBatch("B")
	rep.TransferQueue() <- other
	require.Eventually(t, other.IsReleased, time.Second, time.Millisecond)
	require.EqualValues(t, 2, calls.Load())
	require.False(t, second.IsReleased())
	require.NoError(t, rep.Close())
	require.Eventually(t, second.IsReleased, time.Second, time.Millisecond)
}
