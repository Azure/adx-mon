package metrics

import (
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTableBudgetMetricsRetireDestinations(t *testing.T) {
	c := newTableBudgetCollector()
	budget := wal.NewAdmission(wal.AdmissionOpts{Default: wal.Usage{Bytes: 100, Segments: 2}})
	budget.Restore("a", wal.Destination{Database: "DB", Table: "A"}, 50)
	c.sources["collector"] = budget
	registry := prometheus.NewRegistry()
	require.NoError(t, registry.Register(c))
	families, err := registry.Gather()
	require.NoError(t, err)
	require.Len(t, families, 11)
	for _, f := range families {
		require.Len(t, f.Metric, 1)
		if f.GetName() == "adxmon_table_backlog_bytes" {
			require.EqualValues(t, 50, f.Metric[0].GetGauge().GetValue())
		}
	}
	budget.Forget(wal.Destination{Database: "DB", Table: "A"}, "a")
	families, err = registry.Gather()
	require.NoError(t, err)
	require.Empty(t, families)
}
