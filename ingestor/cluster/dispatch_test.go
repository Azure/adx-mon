package cluster

import (
	"testing"

	"github.com/Azure/adx-mon/metrics"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func deferredCount(t *testing.T, database, table string) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, metrics.IngestorDeferredBatches.WithLabelValues(database, table).Write(&m))
	return m.Counter.GetValue()
}

// A destination that contributes more than its share to one dispatch round has
// the excess left on disk and reported, never dropped silently.
func TestBoundedBatchesDefersAndReports(t *testing.T) {
	before := deferredCount(t, "DB", "Busy")

	first := fairBatch("Busy", "one", 1)
	second := fairBatch("Busy", "two", 1)
	excess := fairBatch("Busy", "three", 1)
	other := fairBatch("Calm", "one", 1)

	result := boundedBatches([]*Batch{first, second, excess, other}, 2)

	require.Equal(t, []*Batch{first, second, other}, result)
	require.True(t, excess.IsReleased(), "excess batch releases its claim for the next scan")
	require.False(t, other.IsReleased(), "unrelated destinations are unaffected")
	require.Equal(t, before+1, deferredCount(t, "DB", "Busy"))
}

// Schema variants of one table share the per-destination budget.
func TestBoundedBatchesSharesBudgetAcrossSchemas(t *testing.T) {
	a := fairBatch("A", "one", 1)
	b := fairBatch("A", "two", 1)
	c := fairBatch("A", "three", 1)
	healthy := fairBatch("B", "one", 1)

	result := boundedBatches([]*Batch{a, b, c, healthy}, 2)

	require.Equal(t, []*Batch{a, b, healthy}, result)
	require.True(t, c.IsReleased())
	require.False(t, healthy.IsReleased())
}
