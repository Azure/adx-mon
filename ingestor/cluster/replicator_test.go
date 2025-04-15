package cluster

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

func TestReplicator_SuccessfulTransfer(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	opts := ReplicatorOpts{
		Partitioner:        &fakePartitioner{owner: "node2", addr: server.URL},
		Health:             &fakeHealthChecker{healthy: true},
		SegmentRemover:     &fakeSegmentRemover{},
		InsecureSkipVerify: true,
		Hostname:           "node1",
		DisableGzip:        true,
	}

	rep, err := NewReplicator(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = rep.Open(ctx)
	require.NoError(t, err)
	defer rep.Close()

	segmentPath := filepath.Join(os.TempDir(), "DB_Table_1.wal")
	require.NoError(t, os.WriteFile(segmentPath, []byte("test"), 0644))
	batch := &Batch{
		Segments: []wal.SegmentInfo{
			{Path: segmentPath},
		},
		batcher: &fakeBatcher{},
	}

	rep.TransferQueue() <- batch
	time.Sleep(time.Second)

	require.NoError(t, rep.Close())

	require.True(t, batch.IsReleased())
	require.True(t, batch.IsRemoved())
}

func TestReplicator_BadRequestError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("invalid file name"))
	}))
	defer server.Close()

	opts := ReplicatorOpts{
		Partitioner:        &fakePartitioner{owner: "node2", addr: server.URL},
		Health:             &fakeHealthChecker{healthy: true},
		SegmentRemover:     &fakeSegmentRemover{},
		InsecureSkipVerify: true,
		Hostname:           "node1",
		DisableGzip:        true,
	}

	rep, err := NewReplicator(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = rep.Open(ctx)
	require.NoError(t, err)
	defer rep.Close()

	segmentPath := filepath.Join(os.TempDir(), "DB_Table_1.wal")
	require.NoError(t, os.WriteFile(segmentPath, []byte("test"), 0644))
	batch := &Batch{
		Segments: []wal.SegmentInfo{
			{Path: segmentPath},
		},
		batcher: &fakeBatcher{},
	}

	rep.TransferQueue() <- batch
	time.Sleep(100 * time.Millisecond)

	require.NoError(t, rep.Close())

	require.True(t, batch.IsReleased())
	require.True(t, batch.IsRemoved())
}

func TestReplicator_SegmentExistsError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()

	opts := ReplicatorOpts{
		Partitioner:        &fakePartitioner{owner: "node2", addr: server.URL},
		Health:             &fakeHealthChecker{healthy: true},
		SegmentRemover:     &fakeSegmentRemover{},
		InsecureSkipVerify: true,
		Hostname:           "node1",
		DisableGzip:        true,
	}

	rep, err := NewReplicator(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = rep.Open(ctx)
	require.NoError(t, err)
	defer rep.Close()

	segmentPath := filepath.Join(os.TempDir(), "DB_Table_1.wal")
	require.NoError(t, os.WriteFile(segmentPath, []byte("test"), 0644))
	batch := &Batch{
		Segments: []wal.SegmentInfo{
			{Path: segmentPath},
		},
		batcher: &fakeBatcher{},
	}

	rep.TransferQueue() <- batch
	time.Sleep(100 * time.Millisecond)

	require.NoError(t, rep.Close())

	require.True(t, batch.IsReleased())
	require.True(t, batch.IsRemoved())
}

func TestReplicator_PeerOverloadedError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	opts := ReplicatorOpts{
		Partitioner:        &fakePartitioner{owner: "node2", addr: server.URL},
		Health:             &fakeHealthChecker{healthy: true},
		SegmentRemover:     &fakeSegmentRemover{},
		InsecureSkipVerify: true,
		Hostname:           "node1",
		DisableGzip:        true,
	}

	rep, err := NewReplicator(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = rep.Open(ctx)
	require.NoError(t, err)
	defer rep.Close()

	segmentPath := filepath.Join(os.TempDir(), "DB_Table_1.wal")
	require.NoError(t, os.WriteFile(segmentPath, []byte("test"), 0644))
	batch := &Batch{
		Segments: []wal.SegmentInfo{
			{Path: segmentPath},
		},
		batcher: &fakeBatcher{},
	}

	rep.TransferQueue() <- batch
	time.Sleep(100 * time.Millisecond)

	require.NoError(t, rep.Close())

	require.True(t, batch.IsReleased())
	require.False(t, batch.IsRemoved())

}

func TestReplicator_UnknownError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	opts := ReplicatorOpts{
		Partitioner:        &fakePartitioner{owner: "node2", addr: server.URL},
		Health:             &fakeHealthChecker{healthy: true},
		SegmentRemover:     &fakeSegmentRemover{},
		InsecureSkipVerify: true,
		Hostname:           "node1",
		DisableGzip:        true,
	}

	rep, err := NewReplicator(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = rep.Open(ctx)
	require.NoError(t, err)
	defer rep.Close()

	segmentPath := filepath.Join(os.TempDir(), "DB_Table_1.wal")
	require.NoError(t, os.WriteFile(segmentPath, []byte("test"), 0644))
	batch := &Batch{
		Segments: []wal.SegmentInfo{
			{Path: segmentPath},
		},
		batcher: &fakeBatcher{},
	}

	rep.TransferQueue() <- batch
	time.Sleep(100 * time.Millisecond)

	require.NoError(t, rep.Close())

	require.True(t, batch.IsReleased())
	require.False(t, batch.IsRemoved())

}
