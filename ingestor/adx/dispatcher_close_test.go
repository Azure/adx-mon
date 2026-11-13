// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package adx

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/stretchr/testify/require"
)

// TestDispatcher_CloseWaitsForDispatchGoroutines verifies that Close() does not
// return until the dispatch goroutines have fully stopped.
//
// Before the fix, Close() only called cancel() and closed the uploaders and
// returned immediately, even though the dispatch goroutine was still running
// (mid-way through dispatching a batch to a blocked uploader). Any batch still
// queued at that point was silently abandoned. This test holds the dispatch
// goroutine mid-work with a gated uploader and asserts that Close() blocks
// until the gate is released.
func TestDispatcher_CloseWaitsForDispatchGoroutines(t *testing.T) {
	u := &blockingUploadQueueUploader{
		Uploader: NewFakeUploader("db"),
		entered:  make(chan struct{}),
		resume:   make(chan struct{}),
	}
	d := NewDispatcher([]Uploader{u})
	require.NoError(t, d.Open(context.Background()))

	// Send a batch so the dispatch goroutine begins dispatching it. The
	// uploader's UploadQueue() call blocks on the gate, pinning the goroutine
	// inside dispatchBatch.
	d.UploadQueue() <- &cluster.Batch{Database: "db", Prefix: "x"}
	select {
	case <-u.entered:
	case <-time.After(time.Second):
		t.Fatal("dispatch goroutine did not reach the uploader")
	}

	// Post-fix, Close() blocks on the WaitGroup join until the dispatch
	// goroutine is released. Pre-fix, Close() returns immediately without
	// waiting for the goroutine, so done fires while the gate is still held.
	closeDone := make(chan error, 1)
	go func() { closeDone <- d.Close() }()

	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before the dispatch goroutine stopped (leak window): %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	// Release the dispatch goroutine; Close should now return.
	close(u.resume)
	select {
	case err := <-closeDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Close did not return after the dispatch goroutine was released")
	}
}

// blockingUploadQueueUploader blocks inside UploadQueue() until the gate is
// released, so the dispatcher's dispatch goroutine is provably mid-work.
type blockingUploadQueueUploader struct {
	Uploader
	entered chan struct{}
	resume  chan struct{}
}

func (b *blockingUploadQueueUploader) UploadQueue() chan *cluster.Batch {
	close(b.entered)
	<-b.resume
	return b.Uploader.UploadQueue()
}
