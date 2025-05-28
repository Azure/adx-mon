package adx

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/storage"
	"github.com/stretchr/testify/require"
)

// TestAuditDiskSpaceTask_ActiveSegmentLeak demonstrates the bug where active segments
// are not counted in the QueueSizer but are present on disk, causing a mismatch.
func TestAuditDiskSpaceTask_ActiveSegmentLeak(t *testing.T) {
	// Create a temporary directory for testing
	storageDir := t.TempDir()

	// Create a local store with small limits to easily trigger the issue
	store := storage.NewLocalStore(storage.StoreOpts{
		StorageDir:     storageDir,
		SegmentMaxSize: 1000, // Large enough to not trigger rotation
		SegmentMaxAge:  time.Hour,
		MaxDiskUsage:   0, // No limits for this test
	})

	require.NoError(t, store.Open(context.Background()))
	defer store.Close()

	// Create a batcher that uses the store's index (simulating the real system)
	batcher := cluster.NewBatcher(cluster.BatcherOpts{
		StorageDir:        storageDir,
		Segmenter:         store.Index(),
		ActiveSegmenter:   store,
		UploadQueue:       make(chan *cluster.Batch, 10),
		TransferQueue:     make(chan *cluster.Batch, 10),
		TransfersDisabled: true,
	})

	require.NoError(t, batcher.Open(context.Background()))
	defer batcher.Close()

	// Create an audit task
	audit := NewAuditDiskSpaceTask(batcher, storageDir)

	// Initial state: no segments should exist
	require.NoError(t, audit.Run(context.Background()))

	// Write some data to create active segments
	// This simulates writing to multiple WALs which will create active segments
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		err := store.WriteTimeSeries(ctx, []*prompb.TimeSeries{
			{
				Labels: []*prompb.Label{
					{Name: []byte("__name__"), Value: []byte("test_metric")},
					{Name: []byte("adxmon_database"), Value: []byte("testdb")},
					{Name: []byte("instance"), Value: []byte("test")},
					{Name: []byte("job"), Value: []byte("test")},
				},
				Samples: []*prompb.Sample{
					{Value: float64(i), Timestamp: time.Now().UnixMilli()},
				},
			},
		})
		require.NoError(t, err)
	}

	// Give it a moment for writes to flush
	time.Sleep(100 * time.Millisecond)

	// Count actual files on disk
	var actualSize int64
	var actualCount int64
	err := filepath.Walk(storageDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(info.Name()) == ".wal" {
			actualSize += info.Size()
			actualCount++
		}
		return nil
	})
	require.NoError(t, err)

	// Get expected counts from the batcher (QueueSizer)
	expectedSize := batcher.SegmentsSize()
	expectedCount := batcher.SegmentsTotal()

	// This demonstrates the bug: active segments exist on disk but aren't counted in the index
	if actualCount > 0 {
		t.Logf("Actual segments on disk: count=%d, size=%d", actualCount, actualSize)
		t.Logf("Expected segments in index: count=%d, size=%d", expectedCount, expectedSize)
		
		// This assertion will fail before the fix, demonstrating the bug
		// After the fix, this should pass
		require.Equal(t, expectedCount, actualCount, "Segment count mismatch indicates the bug")
		require.Equal(t, expectedSize, actualSize, "Segment size mismatch indicates the bug")
	}
}