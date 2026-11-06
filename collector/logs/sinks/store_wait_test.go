package sinks_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector/logs/engine"
	"github.com/Azure/adx-mon/collector/logs/sinks"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/storage"
	"github.com/stretchr/testify/require"
)

// fillingStore writes the first logs of a batch, then reports that storage is full until full is cleared.
type fillingStore struct {
	storage.Store
	mu      sync.Mutex
	written map[*types.Log]int
	full    atomic.Bool
}

func (s *fillingStore) WriteNativeLogs(_ context.Context, batch *types.LogBatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, log := range batch.Logs {
		if s.full.Load() && i == 3 {
			s.full.Store(false)
			var pending []int
			for j := i; j < len(batch.Logs); j++ {
				pending = append(pending, j)
			}
			return &storage.PartialWriteError{Pending: pending, Err: wal.ErrMaxDiskUsageExceeded}
		}
		s.written[log]++
	}
	return nil
}

func (s *fillingStore) counts() (logs, duplicates int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range s.written {
		logs++
		duplicates += n - 1
	}
	return logs, duplicates
}

// When storage fills, a tailing source's batch is written once storage has room instead of being dropped, without
// writing the logs stored before it filled again, and is acked once.
func TestWorkerWaitsForStorage(t *testing.T) {
	store := &fillingStore{written: make(map[*types.Log]int)}
	store.full.Store(true)
	sink, err := sinks.NewStoreSink(sinks.StoreSinkConfig{Store: store})
	require.NoError(t, err)

	var acks atomic.Int64
	batch := &types.LogBatch{Ack: func() { acks.Add(1) }}
	for range 10 {
		batch.Logs = append(batch.Logs, types.NewLog())
	}
	input := make(chan *types.LogBatch, 1)
	input <- batch
	close(input)

	stop := t.Context()
	done := make(chan struct{})
	go func() {
		defer close(done)
		engine.WorkerCreator(nil, []types.Sink{sink})("tail", input).RunWaitingForStorage(stop)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not finish after storage had room")
	}

	logs, duplicates := store.counts()
	t.Logf("stored %d of 10 logs, %d duplicates, acked %d times", logs, duplicates, acks.Load())
	require.Equal(t, 10, logs)
	require.Zero(t, duplicates)
	require.EqualValues(t, 1, acks.Load())
}

// A worker stops waiting for storage when its source shuts down.
func TestWorkerStopsWaitingForStorageOnShutdown(t *testing.T) {
	store := &fillingStore{written: make(map[*types.Log]int)}
	sink, err := sinks.NewStoreSink(sinks.StoreSinkConfig{Store: alwaysFull{store}})
	require.NoError(t, err)
	input := make(chan *types.LogBatch, 1)
	input <- &types.LogBatch{Logs: []*types.Log{types.NewLog()}, Ack: func() { t.Error("batch was acked") }}
	close(input)

	stop, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		engine.WorkerCreator(nil, []types.Sink{sink})("tail", input).RunWaitingForStorage(stop)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker kept waiting after shutdown")
	}
}

// Workers that cannot wait, such as those answering an HTTP request, still fail a batch when storage is full.
func TestWorkerWithoutStorageWaitFailsWhenFull(t *testing.T) {
	sink, err := sinks.NewStoreSink(sinks.StoreSinkConfig{Store: alwaysFull{}})
	require.NoError(t, err)
	start := time.Now()
	_, err = engine.WorkerCreator(nil, []types.Sink{sink})("http", nil).ProcessBatch(context.Background(), &types.LogBatch{Logs: []*types.Log{types.NewLog()}})
	require.ErrorIs(t, err, wal.ErrMaxDiskUsageExceeded)
	require.Less(t, time.Since(start), time.Second)
}

type alwaysFull struct{ storage.Store }

func (alwaysFull) WriteNativeLogs(context.Context, *types.LogBatch) error {
	return &storage.PartialWriteError{Pending: []int{0}, Err: wal.ErrMaxDiskUsageExceeded}
}
