package collector

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A table's segments are sent to the ingestor in batches of up to 4 MiB.
func TestDefaultTransferBatchSize(t *testing.T) {
	require.Equal(t, int64(4*1024*1024), int64(DefaultTransferBatchSize))
}

func TestServiceOptsTransferBatchSize(t *testing.T) {
	require.Equal(t, int64(DefaultTransferBatchSize), (&ServiceOpts{}).transferBatchSize())
	require.Equal(t, int64(DefaultTransferBatchSize), (&ServiceOpts{TransferBatchSize: -1}).transferBatchSize())
	require.Equal(t, int64(9<<20), (&ServiceOpts{TransferBatchSize: 9 << 20}).transferBatchSize())
}

// The replicator merges a table's waiting transfers with its default segment count and no byte bound, whatever the
// collector's own max-batch-segments and transfer-batch-size.
func TestServiceOptsReplicatorOpts(t *testing.T) {
	opts := &ServiceOpts{NodeName: "node", MaxTransferConcurrency: 7, MaxBatchSegments: 100, TransferBatchSize: 9 << 20, DisableGzip: true, QueuedReservedWorkersPercent: 20}
	r := opts.replicatorOpts(nil, nil, nil)
	require.Equal(t, "node", r.Hostname)
	require.Equal(t, 7, r.MaxTransferConcurrency)
	require.True(t, r.DisableGzip)
	require.Equal(t, 20, r.QueuedReservedWorkersPercent)
	require.NotNil(t, r.TransferLatency)
	require.Zero(t, r.MaxBatchSegments)
	require.Zero(t, r.MaxTransferSize)
}
