package cluster

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

// oversizedServer rejects payloads above limit with 413 and records what it
// accepted, modelling a destination whose table budget is smaller than the
// merged batch.
type oversizedServer struct {
	limit    int64
	mu       sync.Mutex
	accepted []int64
	rejected int
}

func (s *oversizedServer) handler(w http.ResponseWriter, r *http.Request) {
	n, _ := io.Copy(io.Discard, r.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	if n > s.limit {
		s.rejected++
		w.Header().Set(MaxTransferBytesHeader, strconv.FormatInt(s.limit, 10))
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	s.accepted = append(s.accepted, n)
	w.WriteHeader(http.StatusAccepted)
}

func (s *oversizedServer) counts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.accepted), s.rejected
}

func newOversizedBatch(t *testing.T, dir string, count int, payload []byte) *Batch {
	t.Helper()
	batch := &Batch{Database: "DB", Table: "A", Prefix: "DB_A_s", batcher: &fakeBatcher{}}
	for n := 0; n < count; n++ {
		seg, err := wal.NewSegment(dir, "DB_A_s")
		require.NoError(t, err)
		_, err = seg.Write(context.Background(), payload)
		require.NoError(t, err)
		require.NoError(t, seg.Close())
		batch.Segments = append(batch.Segments, seg.Info())
	}
	return batch
}

// A merged batch larger than the destination budget must be split and
// delivered, not retried unchanged.
func TestOversizedTransferIsSplitAndDelivered(t *testing.T) {
	seg, err := wal.NewSegment(t.TempDir(), "DB_A_s")
	require.NoError(t, err)
	payload := []byte(strings.Repeat("a", 256))
	_, err = seg.Write(context.Background(), payload)
	require.NoError(t, err)
	require.NoError(t, seg.Close())
	single := seg.Info().Size

	// Accept a single segment but not the merged pair.
	server := &oversizedServer{limit: single + (single / 2)}
	s := httptest.NewServer(http.HandlerFunc(server.handler))
	defer s.Close()

	rep, err := NewReplicator(ReplicatorOpts{
		Partitioner:            &fakePartitioner{owner: "peer", addr: s.URL},
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

	batch := newOversizedBatch(t, t.TempDir(), 4, payload)
	rep.TransferQueue() <- batch
	require.Eventually(t, batch.IsReleased, 5*time.Second, time.Millisecond)

	accepted, rejected := server.counts()
	require.Equal(t, 4, accepted, "every segment is delivered after splitting")
	require.Positive(t, rejected, "the oversized merge was rejected first")
}

// A single segment that exceeds the budget cannot be split. It must be retained
// and reported rather than retried forever.
func TestUndeliverableSegmentIsRetainedAndBackedOff(t *testing.T) {
	server := &oversizedServer{limit: 1}
	s := httptest.NewServer(http.HandlerFunc(server.handler))
	defer s.Close()

	health := NewHealth(HealthOpts{})
	rep, err := NewReplicator(ReplicatorOpts{
		Partitioner:            &fakePartitioner{owner: "peer", addr: s.URL},
		Health:                 health,
		Hostname:               "local",
		MaxTransferConcurrency: 1,
		DisableGzip:            true,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, rep.Open(ctx))
	defer rep.Close()

	batch := newOversizedBatch(t, t.TempDir(), 1, []byte("data"))
	rep.TransferQueue() <- batch
	require.Eventually(t, batch.IsReleased, 5*time.Second, time.Millisecond)

	require.False(t, batch.IsRemoved(), "undeliverable data is retained locally")
	require.True(t, health.IsPeerHealthy("peer"), "an oversized payload is not a peer failure")

	_, rejected := server.counts()
	require.LessOrEqual(t, rejected, maxSplitDepth+1, "the payload is not retried indefinitely")

	// The destination is backed off, so a follow-up batch is not attempted.
	before, _ := server.counts()
	next := newOversizedBatch(t, t.TempDir(), 1, []byte("data"))
	rep.TransferQueue() <- next
	time.Sleep(250 * time.Millisecond)
	after, _ := server.counts()
	require.Equal(t, before, after, "backoff suppresses immediate retries")
}
