package metrics

import (
	"testing"

	adxmetrics "github.com/Azure/adx-mon/metrics"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestRecordHealthStatus(t *testing.T) {
	region := "test-region-record-health-status"
	cleanupHealthStatus(region, healthyReason, "MaxSegmentsExceeded")
	t.Cleanup(func() {
		cleanupHealthStatus(region, healthyReason, "MaxSegmentsExceeded")
	})

	s := &service{region: region}
	s.recordHealthStatus(true, "")

	values := collectHealthStatusByReason(t, region)
	require.Equal(t, map[string]float64{healthyReason: 1}, values)

	s.recordHealthStatus(false, "MaxSegmentsExceeded")

	values = collectHealthStatusByReason(t, region)
	require.Equal(t, map[string]float64{"MaxSegmentsExceeded": 0}, values)
}

func TestNormalizeUnhealthyReason(t *testing.T) {
	require.Equal(t, healthyReason, normalizeUnhealthyReason(""))
	require.Equal(t, "MaxDiskUsageExceeded", normalizeUnhealthyReason("MaxDiskUsageExceeded"))
}

func cleanupHealthStatus(region string, reasons ...string) {
	for _, reason := range reasons {
		adxmetrics.IngestorHealthStatus.DeleteLabelValues(region, reason)
	}
}

func collectHealthStatusByReason(t *testing.T, region string) map[string]float64 {
	t.Helper()

	ch := make(chan prometheus.Metric)
	go func() {
		adxmetrics.IngestorHealthStatus.Collect(ch)
		close(ch)
	}()

	values := map[string]float64{}
	for metric := range ch {
		pb := &dto.Metric{}
		require.NoError(t, metric.Write(pb))

		labels := metricLabels(pb)
		if labels["region"] != region {
			continue
		}

		values[labels["unhealthy_reason"]] = pb.GetGauge().GetValue()
	}

	return values
}

func metricLabels(metric *dto.Metric) map[string]string {
	labels := make(map[string]string, len(metric.GetLabel()))
	for _, label := range metric.GetLabel() {
		labels[label.GetName()] = label.GetValue()
	}
	return labels
}
