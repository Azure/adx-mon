package wal

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A segment must be fully written to disk before it is indexed. Subscribers such as the ingestor's batchers dispatch
// a segment as soon as it is indexed, so reading it then must see every byte the index reports.
func TestWALIndexesSegmentOnlyAfterClose(t *testing.T) {
	for name, release := range map[string]func(w *WAL){
		"rotate": func(w *WAL) {
			w.opts.SegmentMaxSize = 1
			w.rotateSegmentIfNecessary()
		},
		"close active segment": func(w *WAL) { w.closeActiveSegment() },
	} {
		t.Run(name, func(t *testing.T) {
			idx := NewIndex()
			var mu sync.Mutex
			var indexed, onDisk int64
			idx.Subscribe(func(info SegmentInfo) {
				st, err := os.Stat(info.Path)
				require.NoError(t, err)
				mu.Lock()
				indexed, onDisk = info.Size, st.Size()
				mu.Unlock()
			})

			// Nothing is flushed in the background, so only closing the segment writes its data to disk.
			w := newTestWAL(t, WALOpts{Index: idx, SegmentMaxSize: 1 << 20, WALFlushInterval: time.Hour})
			for range 10 {
				require.NoError(t, w.Write(context.Background(), []byte("some sample data")))
			}
			release(w)

			mu.Lock()
			defer mu.Unlock()
			t.Logf("when indexed: index reports %d bytes, %d bytes on disk", indexed, onDisk)
			require.Positive(t, indexed)
			require.Equal(t, indexed, onDisk, "segment was indexed before its data was on disk")
		})
	}
}
