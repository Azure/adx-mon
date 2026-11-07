package collector

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A table's segments are sent to the ingestor in batches of up to 4 MiB.
func TestDefaultTransferBatchSize(t *testing.T) {
	require.Equal(t, int64(4*1024*1024), int64(DefaultTransferBatchSize))
}
