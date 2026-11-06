package cluster

import (
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

// stalledPeerTransfer sends one batch of size bytes to a peer served by handler and returns how long the transfer
// held its worker before giving up.
func stalledPeerTransfer(t *testing.T, size int, handler func(w http.ResponseWriter, r *http.Request)) time.Duration {
	t.Helper()
	release := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler(w, r)
		<-release
	}))
	defer s.Close()
	defer close(release)

	rep, err := NewReplicator(ReplicatorOpts{
		Partitioner: &fakePartitioner{owner: "remote", addr: s.URL},
		Health:      NewHealth(HealthOpts{}),
		Hostname:    "local",
		DisableGzip: true,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, rep.Open(ctx))
	defer rep.Close()

	seg, err := wal.NewSegment(t.TempDir(), "DB_Table")
	require.NoError(t, err)
	payload := make([]byte, size)
	_, _ = rand.Read(payload)
	_, err = seg.Write(ctx, payload)
	require.NoError(t, err)
	require.NoError(t, seg.Close())

	batch := &Batch{Database: "DB", Table: "Table", Segments: []wal.SegmentInfo{seg.Info()}, batcher: &fakeBatcher{}}
	start := time.Now()
	rep.TransferQueue() <- batch
	require.Eventually(t, batch.IsReleased, time.Minute, 10*time.Millisecond)
	require.False(t, batch.IsRemoved())
	return time.Since(start)
}

// A peer that receives the body but never responds releases the worker after the response header timeout.
func TestReplicatorAbandonsPeerThatDoesNotRespond(t *testing.T) {
	defer func(d time.Duration) { transferResponseHeaderTimeout = d }(transferResponseHeaderTimeout)
	transferResponseHeaderTimeout = 500 * time.Millisecond

	held := stalledPeerTransfer(t, 1024, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
	})
	t.Logf("a peer that never responded held the worker for %s", held.Round(100*time.Millisecond))
	require.Less(t, held, 3*time.Second)
}

// A peer that stops reading the body releases the worker once the transfer exceeds the time its size allows.
func TestReplicatorAbandonsPeerThatStopsReading(t *testing.T) {
	defer func(d time.Duration, r int64) { transferBaseTimeout, transferMinBytesPerSecond = d, r }(transferBaseTimeout, transferMinBytesPerSecond)
	transferBaseTimeout, transferMinBytesPerSecond = 500*time.Millisecond, 1<<30

	held := stalledPeerTransfer(t, 32<<20, func(w http.ResponseWriter, r *http.Request) {
		_, _ = r.Body.Read(make([]byte, 1))
	})
	t.Logf("a peer that stopped reading a 32MB transfer held the worker for %s", held.Round(100*time.Millisecond))
	require.Less(t, held, 3*time.Second)
}

func TestTransferTimeoutScalesWithSize(t *testing.T) {
	require.Equal(t, 5*time.Second, transferTimeout(0))
	require.Equal(t, 15*time.Second, transferTimeout(100<<20))
}

// A deadline that fires after the peer accepted a transfer must not turn the success into a timeout, or the segments
// would be kept and sent again.
func TestTransferErrorKeepsSuccessAfterDeadline(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errTransferTimeout)

	require.NoError(t, transferError(ctx, 0, nil))

	err := transferError(ctx, 0, context.Canceled)
	require.ErrorIs(t, err, errTransferTimeout)
	require.ErrorIs(t, err, context.Canceled)
}

// Waiting for a connection behind other transfers to the same peer does not count against a transfer's deadline, so
// a busy but healthy peer is not marked unhealthy.
func TestTransferDeadlineExcludesConnectionWait(t *testing.T) {
	defer func(d time.Duration) { transferBaseTimeout = d }(transferBaseTimeout)
	transferBaseTimeout = time.Second

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		time.Sleep(800 * time.Millisecond)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer s.Close()

	health := NewHealth(HealthOpts{})
	rep, err := NewReplicator(ReplicatorOpts{
		Partitioner:            &fakePartitioner{owner: "remote", addr: s.URL},
		Health:                 health,
		Hostname:               "local",
		MaxTransferConcurrency: 10,
		DisableGzip:            true,
	})
	require.NoError(t, err)
	ctx := t.Context()
	require.NoError(t, rep.Open(ctx))
	defer rep.Close()

	// Ten transfers share the default five connections per peer, so the second five wait about 800ms for one.
	dir := t.TempDir()
	var batches []*Batch
	for i := range 10 {
		seg, err := wal.NewSegment(dir, "DB_Table"+string(rune('A'+i)))
		require.NoError(t, err)
		_, err = seg.Write(ctx, []byte("payload"))
		require.NoError(t, err)
		require.NoError(t, seg.Close())
		b := &Batch{Database: "DB", Table: "Table" + string(rune('A'+i)), Segments: []wal.SegmentInfo{seg.Info()}, batcher: &fakeBatcher{}}
		batches = append(batches, b)
		rep.TransferQueue() <- b
	}
	start := time.Now()
	for _, b := range batches {
		require.Eventually(t, b.IsReleased, 10*time.Second, 10*time.Millisecond)
		require.True(t, b.IsRemoved(), "a transfer waiting for a connection timed out")
	}
	t.Logf("10 transfers over 5 connections delivered in %s", time.Since(start).Round(100*time.Millisecond))
	require.Greater(t, time.Since(start), transferBaseTimeout, "the second wave should have waited for a connection")
	require.True(t, health.IsPeerHealthy("remote"))
}
