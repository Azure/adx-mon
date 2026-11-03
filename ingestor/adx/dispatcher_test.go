// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package adx

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/stretchr/testify/require"
)

func newTestDispatcher(t *testing.T, uploaders ...Uploader) *dispatcher {
	t.Helper()
	d := NewDispatcher(uploaders)
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	go d.upload(ctx)
	t.Cleanup(cancel)
	return d
}

type blockedRealtimeQueueUploader struct {
	Uploader
	entered    chan struct{}
	resume     chan struct{}
	resumeOnce sync.Once
}

func (u *blockedRealtimeQueueUploader) RealtimeUploadQueue() chan *cluster.Batch {
	close(u.entered)
	<-u.resume
	return u.Uploader.RealtimeUploadQueue()
}

func (u *blockedRealtimeQueueUploader) unblock() {
	u.resumeOnce.Do(func() { close(u.resume) })
}

func TestDispatcher_RoutesByPriority(t *testing.T) {
	u := NewFakeUploader("db")
	d := newTestDispatcher(t, u)

	queued := &cluster.Batch{Database: "db", Prefix: "queued"}
	realtime := &cluster.Batch{Database: "db", Prefix: "realtime", Priority: ingestpolicy.PriorityRealtime}
	d.UploadQueue() <- queued
	d.RealtimeUploadQueue() <- realtime

	require.Eventually(t, func() bool {
		return len(u.UploadQueue()) == 1 && len(u.RealtimeUploadQueue()) == 1
	}, time.Second, time.Millisecond)
	require.Same(t, queued, <-u.UploadQueue())
	require.Same(t, realtime, <-u.RealtimeUploadQueue())
}

func TestDispatcher_RealtimeBatchOnDefaultQueue(t *testing.T) {
	// The batcher falls back to the default queue when no realtime queue is configured; the batch priority still
	// determines the uploader queue.
	u := NewFakeUploader("db")
	d := newTestDispatcher(t, u)

	realtime := &cluster.Batch{Database: "db", Priority: ingestpolicy.PriorityRealtime}
	d.UploadQueue() <- realtime

	require.Eventually(t, func() bool { return len(u.RealtimeUploadQueue()) == 1 }, time.Second, time.Millisecond)
	require.Empty(t, u.UploadQueue())
}

func TestDispatcher_RoutesByDatabase(t *testing.T) {
	a, b := NewFakeUploader("a"), NewFakeUploader("b")
	d := newTestDispatcher(t, a, b)

	d.RealtimeUploadQueue() <- &cluster.Batch{Database: "a", Priority: ingestpolicy.PriorityRealtime}
	d.UploadQueue() <- &cluster.Batch{Database: "b"}
	// Batches for unknown databases are dropped.
	d.UploadQueue() <- &cluster.Batch{Database: "unknown"}

	require.Eventually(t, func() bool {
		return len(a.RealtimeUploadQueue()) == 1 && len(b.UploadQueue()) == 1
	}, time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return len(d.UploadQueue()) == 0 }, time.Second, time.Millisecond)
	require.Empty(t, a.UploadQueue())
	require.Empty(t, b.RealtimeUploadQueue())
}

func TestDispatcher_QueuedPathContinuesWhileRealtimePathIsBlocked(t *testing.T) {
	base := NewFakeUploader("db")
	u := &blockedRealtimeQueueUploader{
		Uploader: base,
		entered:  make(chan struct{}),
		resume:   make(chan struct{}),
	}
	d := newTestDispatcher(t, u)
	t.Cleanup(u.unblock)

	realtime := &cluster.Batch{Database: "db", Priority: ingestpolicy.PriorityRealtime}
	d.RealtimeUploadQueue() <- realtime
	select {
	case <-u.entered:
	case <-time.After(time.Second):
		t.Fatal("realtime dispatcher did not reach uploader")
	}

	queued := &cluster.Batch{Database: "db", Priority: ingestpolicy.PriorityQueued}
	d.UploadQueue() <- queued
	require.Eventually(t, func() bool { return len(base.UploadQueue()) == 1 }, time.Second, time.Millisecond)
	require.Same(t, queued, <-base.UploadQueue())

	u.unblock()
	require.Eventually(t, func() bool { return len(base.RealtimeUploadQueue()) == 1 }, time.Second, time.Millisecond)
	require.Same(t, realtime, <-base.RealtimeUploadQueue())
}

// TestDispatcher_UnknownDatabaseReleasesBatch verifies that a batch for a database without an uploader is released.  A
// batch that is never released keeps its segments marked as batched, so they are never batched again, never uploaded
// and never removed: the oldest segment age grows without bound.
func TestDispatcher_UnknownDatabaseReleasesBatch(t *testing.T) {
	u := NewFakeUploader("db")
	d := NewDispatcher([]Uploader{u})
	d.unroutableDelay = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go d.upload(ctx)

	batch := &cluster.Batch{Database: "unknown", Prefix: "unknown_table"}
	d.UploadQueue() <- batch

	// The batch is held so it is not rebuilt and redispatched on every sweep, then released so it can be retried.
	time.Sleep(10 * time.Millisecond)
	require.False(t, batch.IsReleased())
	require.Eventually(t, batch.IsReleased, time.Second, time.Millisecond)
	require.Empty(t, u.UploadQueue())
}
