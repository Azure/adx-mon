package collector

import (
	"testing"
	"time"

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

// The replicator merges a table's waiting transfers up to the collector's max-batch-segments and max-transfer-size.
func TestServiceOptsReplicatorOpts(t *testing.T) {
	opts := &ServiceOpts{NodeName: "node", MaxTransferConcurrency: 7, MaxBatchSegments: 100, TransferBatchSize: 9 << 20, DisableGzip: true, QueuedReservedWorkersPercent: 20}
	r := opts.replicatorOpts(nil, nil, nil)
	require.Equal(t, "node", r.Hostname)
	require.Equal(t, 7, r.MaxTransferConcurrency)
	require.True(t, r.DisableGzip)
	require.Equal(t, 20, r.QueuedReservedWorkersPercent)
	require.NotNil(t, r.TransferLatency)
	require.Equal(t, 100, r.MaxBatchSegments)
	require.Equal(t, int64(DefaultMaxTransferSize), r.MaxTransferSize)

	r = (&ServiceOpts{MaxTransferSize: 50 << 20}).replicatorOpts(nil, nil, nil)
	require.Zero(t, r.MaxBatchSegments, "the replicator's own default")
	require.Equal(t, int64(50<<20), r.MaxTransferSize)
}

// Merged transfers are bounded by the ingestor's default max-transfer-size.
func TestDefaultMaxTransferSize(t *testing.T) {
	require.Equal(t, int64(100*1024*1024), int64(DefaultMaxTransferSize))
	require.Equal(t, int64(DefaultMaxTransferSize), (&ServiceOpts{MaxTransferSize: -1}).maxTransferSize())
}

// A failed transfer stops the collector's transfers for at most a minute.
func TestMaxTransferRetryDelay(t *testing.T) {
	require.Equal(t, time.Minute, maxTransferRetryDelay)
}
