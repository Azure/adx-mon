package adx

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/schema"
	"github.com/stretchr/testify/require"
)

// One destination stuck in an upload must not consume every worker: a healthy
// destination has to keep making progress.
func TestADXUploadWorkersIsolateSlowTable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	n := NewUploader(nil, UploaderOpts{Database: "DB", ConcurrentUploads: 2})
	started := make(chan string, 10)
	n.queuedUpload = func(_ context.Context, _ io.Reader, _, table string, _ schema.SchemaMapping) error {
		started <- table
		if table == "A" {
			<-ctx.Done()
		}
		return errors.New("retryable backend failure")
	}
	n.startWorkers(ctx)

	makeBatch := func(table string) *cluster.Batch {
		seg, err := wal.NewSegment(t.TempDir(), "DB_"+table)
		require.NoError(t, err)
		_, err = seg.Write(ctx, []byte("1,2\n"))
		require.NoError(t, err)
		require.NoError(t, seg.Close())
		return &cluster.Batch{Database: "DB", Table: table, Segments: []wal.SegmentInfo{seg.Info()}}
	}

	a := makeBatch("A")
	n.queue <- a
	require.Equal(t, "A", <-started)

	n.queue <- makeBatch("A")
	b := makeBatch("B")
	n.queue <- b

	select {
	case table := <-started:
		require.Equal(t, "B", table)
	case <-time.After(5 * time.Second):
		t.Fatal("healthy table starved behind a stuck upload")
	}

	require.Eventually(t, b.IsReleased, 5*time.Second, time.Millisecond)
	require.False(t, b.IsRemoved(), "a failed upload keeps its segments")
}
