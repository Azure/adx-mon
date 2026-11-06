package wal

import (
	"context"
	"crypto/rand"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// Writers that fill a segment at the same time rotate it once, rather than each creating a next segment that all
// but one then close and remove, and none of their writes fail because another writer filled the next segment first.
func TestWAL_ConcurrentSizeRotationCreatesOneSegment(t *testing.T) {
	var created atomic.Int64
	testHookSegmentCreated = func() { created.Add(1) }
	defer func() { testHookSegmentCreated = nil }()

	w := newTestWAL(t, WALOpts{SegmentMaxSize: 64 << 10})
	// Random data so each block is its full size after compression.
	buf := make([]byte, 1024)
	_, _ = rand.Read(buf)
	var wg sync.WaitGroup
	var failed atomic.Int64
	for range 20 {
		wg.Go(func() {
			for range 200 {
				if err := w.Write(context.Background(), buf); err != nil {
					failed.Add(1)
				}
			}
		})
	}
	wg.Wait()

	kept := int64(w.index.TotalSegments())
	if w.Segment() != nil {
		kept++
	}
	t.Logf("created %d segments, kept %d, discarded %d; %d of 4000 writes failed", created.Load(), kept, created.Load()-kept, failed.Load())
	require.Zero(t, failed.Load())
	require.LessOrEqual(t, created.Load()-kept, int64(1), "segments were created and discarded")
}
