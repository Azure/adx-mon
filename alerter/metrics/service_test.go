package metrics

import (
	"testing"

	adxmetrics "github.com/Azure/adx-mon/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestGatherSnapshotDelta(t *testing.T) {
	registry := prometheus.NewRegistry()
	evaluations := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "adxmon", Subsystem: "alerter", Name: "alert_rule_evaluations_total",
	}, []string{"outcome"})
	alerts := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "adxmon", Subsystem: "alerter", Name: "alerts_generated_total",
	})
	durations := prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "adxmon", Subsystem: "alerter", Name: "alert_rule_evaluation_duration_seconds",
	})
	registry.MustRegister(evaluations, alerts, durations)

	evaluations.WithLabelValues(adxmetrics.AlertRuleEvaluationOutcomeSuccess).Add(2)
	alerts.Add(3)
	durations.Observe(2)
	durations.Observe(4)
	previous, err := gatherSnapshot(registry)
	require.NoError(t, err)

	evaluations.WithLabelValues(adxmetrics.AlertRuleEvaluationOutcomeSuccess).Inc()
	evaluations.WithLabelValues(adxmetrics.AlertRuleEvaluationOutcomeServiceError).Inc()
	alerts.Add(2)
	durations.Observe(3)
	durations.Observe(5)
	current, err := gatherSnapshot(registry)
	require.NoError(t, err)

	delta := current.delta(previous)
	require.Equal(t, float64(1), delta.evaluations[adxmetrics.AlertRuleEvaluationOutcomeSuccess])
	require.Equal(t, float64(1), delta.evaluations[adxmetrics.AlertRuleEvaluationOutcomeServiceError])
	require.Equal(t, float64(2), delta.totalEvaluations())
	require.Equal(t, float64(2), delta.alertsGenerated)
	require.Equal(t, uint64(2), delta.durationCount)
	require.Equal(t, float64(4), delta.averageDurationSeconds())
}

func TestSnapshotDeltaHandlesCounterReset(t *testing.T) {
	previous := snapshot{
		evaluations:     map[string]float64{adxmetrics.AlertRuleEvaluationOutcomeSuccess: 10},
		alertsGenerated: 8,
		durationCount:   10,
		durationSum:     20,
	}
	current := snapshot{
		evaluations:     map[string]float64{adxmetrics.AlertRuleEvaluationOutcomeSuccess: 2},
		alertsGenerated: 1,
		durationCount:   2,
		durationSum:     3,
	}

	delta := current.delta(previous)
	require.Equal(t, float64(2), delta.evaluations[adxmetrics.AlertRuleEvaluationOutcomeSuccess])
	require.Equal(t, float64(1), delta.alertsGenerated)
	require.Equal(t, uint64(2), delta.durationCount)
	require.Equal(t, float64(1.5), delta.averageDurationSeconds())
}
