package metrics

import (
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestTableWorkMetricsAndRegistrationLifetime(t *testing.T) {
	read := func() map[wal.Destination]TableWork {
		return map[wal.Destination]TableWork{{Database: "DB", Table: "A"}: {Queued: 2, Running: 1, RetryAt: time.Now().Add(time.Second)}}
	}
	c := &tableWorkCollector{sources: map[string]*tableWorkSource{"test": {read: read}}, descriptions: tableWorkMetrics.descriptions}
	r := prometheus.NewRegistry()
	require.NoError(t, r.Register(c))
	families, err := r.Gather()
	require.NoError(t, err)
	require.Len(t, families, 3)
	for _, f := range families {
		if f.GetName() == "adxmon_table_work_queued_batches" {
			require.EqualValues(t, 2, f.Metric[0].GetGauge().GetValue())
		}
	}
	old := RegisterTableWork("registration-test", read)
	current := RegisterTableWork("registration-test", read)
	old()
	tableWorkMetrics.mu.Lock()
	require.Contains(t, tableWorkMetrics.sources, "registration-test")
	tableWorkMetrics.mu.Unlock()
	current()
	tableWorkMetrics.mu.Lock()
	require.NotContains(t, tableWorkMetrics.sources, "registration-test")
	tableWorkMetrics.mu.Unlock()
}
