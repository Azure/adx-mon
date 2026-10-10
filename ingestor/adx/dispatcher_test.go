// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package adx

import (
	"context"
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

func TestDispatcher_DispatchesRealtimeFirst(t *testing.T) {
	// Share one uploader queue so the dispatch order is observable.
	u := NewFakeUploader("db").(*fakeUploader)
	u.realtimeQueue = u.queue
	d := NewDispatcher([]Uploader{u})

	// Fill both queues before the dispatcher starts so it must choose between them.
	for i := 0; i < 5; i++ {
		d.UploadQueue() <- &cluster.Batch{Database: "db"}
		d.RealtimeUploadQueue() <- &cluster.Batch{Database: "db", Priority: ingestpolicy.PriorityRealtime}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.upload(ctx)

	var got []ingestpolicy.Priority
	for i := 0; i < 10; i++ {
		select {
		case b := <-u.queue:
			got = append(got, b.Priority)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for dispatched batches")
		}
	}
	rt, q := ingestpolicy.PriorityRealtime, ingestpolicy.PriorityQueued
	require.Equal(t, []ingestpolicy.Priority{rt, rt, rt, rt, rt, q, q, q, q, q}, got)
}
