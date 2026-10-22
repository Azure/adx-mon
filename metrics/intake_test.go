package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

type fakeIntake struct{ inFlight, limit int64 }

func (f fakeIntake) InFlight() int64 { return f.inFlight }
func (f fakeIntake) Limit() int64    { return f.limit }

func TestIntakeMemoryMetrics(t *testing.T) {
	c := newIntakeCollector()
	c.sources["ingestor"] = fakeIntake{inFlight: 300, limit: 1024}
	registry := prometheus.NewRegistry()
	require.NoError(t, registry.Register(c))

	families, err := registry.Gather()
	require.NoError(t, err)
	values := make(map[string]float64)
	for _, f := range families {
		require.Len(t, f.Metric, 1)
		values[f.GetName()] = f.Metric[0].GetGauge().GetValue()
	}
	require.Equal(t, map[string]float64{"adxmon_intake_memory_bytes": 300, "adxmon_intake_memory_limit_bytes": 1024}, values)

	delete(c.sources, "ingestor")
	families, err = registry.Gather()
	require.NoError(t, err)
	require.Empty(t, families)
}
