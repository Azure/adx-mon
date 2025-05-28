package adx

import (
	"context"
	"testing"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

// TestAuditDiskSpaceTask_NilActiveSegmenter tests that the batcher handles
// nil ActiveSegmenter gracefully (for backwards compatibility)
func TestAuditDiskSpaceTask_NilActiveSegmenter(t *testing.T) {
	// Create a batcher without ActiveSegmenter (simulating old configurations)
	batcher := cluster.NewBatcher(cluster.BatcherOpts{
		StorageDir:        t.TempDir(),
		Segmenter:         &fakeSegmenter{},
		ActiveSegmenter:   nil, // This should be handled gracefully
		UploadQueue:       make(chan *cluster.Batch, 10),
		TransferQueue:     make(chan *cluster.Batch, 10),
		TransfersDisabled: true,
	})

	require.NoError(t, batcher.Open(context.Background()))
	defer batcher.Close()

	// These methods should not panic when ActiveSegmenter is nil
	require.Equal(t, int64(0), batcher.SegmentsTotal())
	require.Equal(t, int64(0), batcher.SegmentsSize())

	// Create an audit task
	audit := NewAuditDiskSpaceTask(batcher, t.TempDir())

	// This should not panic
	require.NoError(t, audit.Run(context.Background()))
}

type fakeSegmenter struct{}

func (f *fakeSegmenter) Get(infos []wal.SegmentInfo, prefix string) []wal.SegmentInfo {
	return infos[:0]
}

func (f *fakeSegmenter) PrefixesByAge() []string {
	return nil
}

func (f *fakeSegmenter) Remove(si wal.SegmentInfo) {
}