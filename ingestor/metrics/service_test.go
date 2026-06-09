package metrics

import (
	"testing"
	"time"

	adxmetrics "github.com/Azure/adx-mon/metrics"
	"github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestService_ReportHealth(t *testing.T) {
	region := "test-report-health"
	s := &service{
		health: &fakeHealthReporter{},
		region: region,
	}

	healthy, unhealthyReason := s.reportHealth()
	require.True(t, healthy)
	require.Empty(t, unhealthyReason)
	require.Equal(t, float64(1), ingestorHealthCheckValue(t, region))

	s.health = &fakeHealthReporter{unhealthyReason: "MaxDiskUsageExceeded"}

	healthy, unhealthyReason = s.reportHealth()
	require.False(t, healthy)
	require.Equal(t, "MaxDiskUsageExceeded", unhealthyReason)
	require.Equal(t, float64(0), ingestorHealthCheckValue(t, region))
}

func ingestorHealthCheckValue(t *testing.T, region string) float64 {
	t.Helper()

	m := &io_prometheus_client.Metric{}
	require.NoError(t, adxmetrics.IngestorHealthCheck.WithLabelValues(region).Write(m))
	return m.GetGauge().GetValue()
}

type fakeHealthReporter struct {
	unhealthyReason string
}

func (f *fakeHealthReporter) IsHealthy() bool {
	return f.unhealthyReason == ""
}

func (f *fakeHealthReporter) TransferQueueSize() int {
	return 0
}

func (f *fakeHealthReporter) UploadQueueSize() int {
	return 0
}

func (f *fakeHealthReporter) SegmentsTotal() int64 {
	return 0
}

func (f *fakeHealthReporter) SegmentsSize() int64 {
	return 0
}

func (f *fakeHealthReporter) UnhealthyReason() string {
	return f.unhealthyReason
}

func (f *fakeHealthReporter) MaxSegmentAge() time.Duration {
	return 0
}
