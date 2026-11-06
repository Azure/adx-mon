package cluster

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

// transferSegment writes a closed segment for DB_Table holding value and returns it.
func transferSegment(t *testing.T, dir, value string) wal.SegmentInfo {
	t.Helper()
	s, err := wal.NewSegment(dir, "DB_Table")
	require.NoError(t, err)
	_, err = s.Write(context.Background(), []byte(value))
	require.NoError(t, err)
	require.NoError(t, s.Close())
	return wal.SegmentInfo{Prefix: "DB_Table", Path: s.Path(), Size: s.Size()}
}

// With one transfer per table, batches for a table that arrive while its transfer runs are sent together in the
// next transfer.
func TestReplicator_MergesBatchesBehindRunningTransfer(t *testing.T) {
	var (
		mu       sync.Mutex
		received []string
	)
	first := make(chan struct{})
	unblock := make(chan struct{})
	var once sync.Once
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		received = append(received, r.URL.Query().Get("filename"))
		mu.Unlock()
		once.Do(func() { close(first); <-unblock })
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	rep, err := NewReplicator(ReplicatorOpts{
		Partitioner:            &fakePartitioner{owner: "node2", addr: server.URL},
		Health:                 &fakeHealthChecker{healthy: true},
		SegmentRemover:         &fakeSegmentRemover{},
		InsecureSkipVerify:     true,
		Hostname:               "node1",
		DisableGzip:            true,
		MaxTransferConcurrency: 4,
		MaxTransfersPerTable:   1,
	})
	require.NoError(t, err)
	require.NoError(t, rep.Open(context.Background()))
	defer rep.Close()

	dir := t.TempDir()
	rec := &releaseRecorder{}
	batch := func(value string) (*Batch, wal.SegmentInfo) {
		si := transferSegment(t, dir, value)
		return &Batch{Database: "DB", Table: "Table", Prefix: "DB_Table", Segments: []wal.SegmentInfo{si}, batcher: rec}, si
	}

	b1, s1 := batch("1")
	rep.TransferQueue() <- b1
	<-first
	b2, s2 := batch("2")
	b3, s3 := batch("3")
	b4, s4 := batch("4")
	for _, b := range []*Batch{b2, b3, b4} {
		rep.TransferQueue() <- b
	}
	// Give the scheduler time to take the batches in before the running transfer finishes.
	time.Sleep(50 * time.Millisecond)
	close(unblock)

	require.Eventually(t, func() bool { return len(rec.releases()) == 2 }, 5*time.Second, 10*time.Millisecond)
	mu.Lock()
	require.Equal(t, []string{filepath.Base(s1.Path), filepath.Base(s2.Path)}, received)
	mu.Unlock()
	require.Equal(t, [][]string{{s1.Path}, {s2.Path, s3.Path, s4.Path}}, rec.releases())
	require.True(t, b1.IsRemoved())
	require.True(t, b2.IsRemoved())
}

// Without a per-table limit a table's next batch is sent while its previous transfer is still running.
func TestReplicator_SendsBatchesConcurrentlyWithoutPerTableLimit(t *testing.T) {
	requests := make(chan struct{}, 10)
	unblock := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		requests <- struct{}{}
		<-unblock
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	defer close(unblock)

	rep, err := NewReplicator(ReplicatorOpts{
		Partitioner:            &fakePartitioner{owner: "node2", addr: server.URL},
		Health:                 &fakeHealthChecker{healthy: true},
		SegmentRemover:         &fakeSegmentRemover{},
		InsecureSkipVerify:     true,
		Hostname:               "node1",
		DisableGzip:            true,
		MaxTransferConcurrency: 4,
	})
	require.NoError(t, err)
	require.NoError(t, rep.Open(context.Background()))
	defer rep.Close()

	dir := t.TempDir()
	rec := &releaseRecorder{}
	for _, v := range []string{"1", "2"} {
		si := transferSegment(t, dir, v)
		rep.TransferQueue() <- &Batch{Database: "DB", Table: "Table", Prefix: "DB_Table", Segments: []wal.SegmentInfo{si}, batcher: rec}
		select {
		case <-requests:
		case <-time.After(5 * time.Second):
			t.Fatalf("transfer of batch %s did not start while the previous one was running", v)
		}
	}
}

func TestNewReplicatorMergeLimits(t *testing.T) {
	for name, tc := range map[string]struct {
		opts     ReplicatorOpts
		segments int
		bytes    int64
	}{
		"defaults": {ReplicatorOpts{}, DefaultMaxSegmentCount, 0},
		"set":      {ReplicatorOpts{MaxBatchSegments: 7, MaxTransferSize: 1 << 20}, 7, 1 << 20},
		"disabled": {ReplicatorOpts{MaxBatchSegments: -1}, 0, 0},
	} {
		t.Run(name, func(t *testing.T) {
			r, err := NewReplicator(tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.segments, r.(*replicator).mergeSegments)
			require.Equal(t, tc.bytes, r.(*replicator).mergeBytes)
		})
	}
}

// tablePartitioner assigns each table prefix to an owner, each served at its own address.
type tablePartitioner struct {
	addrs  map[string]string
	owners map[string]string
}

func (p tablePartitioner) Owner(key []byte) (string, string) {
	owner := p.owners[string(key)]
	return owner, p.addrs[owner]
}

// No more transfers to a peer run at once than the replicator has connections to it, while transfers to other peers
// still run.  Batches waiting for a connection stay queued, so later batches for their tables merge into them.
func TestReplicator_LimitsTransfersPerPeerToConnections(t *testing.T) {
	var (
		mu       sync.Mutex
		inFlight = map[string]int{}
		maxSeen  = map[string]int{}
		requests = map[string]int{}
	)
	unblock := make(chan struct{})
	release := sync.OnceFunc(func() { close(unblock) })
	addrs := map[string]string{}
	for _, peer := range []string{"peer1", "peer2"} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			mu.Lock()
			requests[peer]++
			inFlight[peer]++
			maxSeen[peer] = max(maxSeen[peer], inFlight[peer])
			mu.Unlock()
			<-unblock
			mu.Lock()
			inFlight[peer]--
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
		}))
		defer server.Close()
		addrs[peer] = server.URL
	}
	// Runs before the servers close, so a failed assertion does not leave their handlers blocked.
	defer release()

	owners := map[string]string{}
	for i := 0; i < 8; i++ {
		owners[fmt.Sprintf("DB_PeerOne%d", i)] = "peer1"
		owners[fmt.Sprintf("DB_PeerTwo%d", i)] = "peer2"
	}
	rep, err := NewReplicator(ReplicatorOpts{
		Partitioner:            tablePartitioner{addrs: addrs, owners: owners},
		Health:                 &fakeHealthChecker{healthy: true},
		SegmentRemover:         &fakeSegmentRemover{},
		InsecureSkipVerify:     true,
		Hostname:               "node1",
		DisableGzip:            true,
		MaxTransferConcurrency: 20,
	})
	require.NoError(t, err)
	require.NoError(t, rep.Open(context.Background()))
	defer rep.Close()

	dir := t.TempDir()
	rec := &releaseRecorder{}
	send := func(prefix string) {
		s, err := wal.NewSegment(dir, prefix)
		require.NoError(t, err)
		_, err = s.Write(context.Background(), []byte("x"))
		require.NoError(t, err)
		require.NoError(t, s.Close())
		db, table, _, _, err := wal.ParseFilename(s.Path())
		require.NoError(t, err)
		rep.TransferQueue() <- &Batch{Database: db, Table: table, Prefix: prefix,
			Segments: []wal.SegmentInfo{{Prefix: prefix, Path: s.Path(), Size: s.Size()}}, batcher: rec}
	}
	for prefix := range owners {
		send(prefix)
	}

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return inFlight["peer1"] == transferConnsPerPeer && inFlight["peer2"] == transferConnsPerPeer
	}, 5*time.Second, 5*time.Millisecond, "each peer runs as many transfers as it has connections")
	// A second batch for every table: those whose first batch is still waiting merge into it.
	for prefix := range owners {
		send(prefix)
	}
	time.Sleep(100 * time.Millisecond)
	release()
	segments := func() (n int) {
		for _, r := range rec.releases() {
			n += len(r)
		}
		return n
	}
	require.Eventually(t, func() bool { return segments() == 2*len(owners) }, 5*time.Second, 10*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, map[string]int{"peer1": transferConnsPerPeer, "peer2": transferConnsPerPeer}, maxSeen)
	// Per peer: 5 running first batches, 3 waiting first batches each merged with their second, and 5 second batches.
	require.Equal(t, map[string]int{"peer1": 13, "peer2": 13}, requests)
}
