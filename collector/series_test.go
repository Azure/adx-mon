package collector

import (
	"testing"

	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestSeriesCreator(t *testing.T) {
	sc := seriesCreator{}

	m := &io_prometheus_client.Metric{
		Label: []*io_prometheus_client.LabelPair{
			{
				Name:  strPtr("label1"),
				Value: strPtr("value1"),
			},
		},
	}
	series := sc.newSeries("test", ScrapeTarget{}, m)
	require.Equal(t, 2, len(series.Labels))
	require.Equal(t, "__name__", string(series.Labels[0].Name))
	require.Equal(t, "test", string(series.Labels[0].Value))
	require.Equal(t, "label1", string(series.Labels[1].Name))
	require.Equal(t, "value1", string(series.Labels[1].Value))
}

func TestSeriesCreator_MixedCase(t *testing.T) {
	sc := seriesCreator{}

	m := &io_prometheus_client.Metric{
		Label: []*io_prometheus_client.LabelPair{
			{
				Name:  strPtr("Capital"),
				Value: strPtr("value1"),
			},
		},
	}
	series := sc.newSeries("test", ScrapeTarget{}, m)
	require.Equal(t, 2, len(series.Labels))
	require.Equal(t, "__name__", string(series.Labels[0].Name))
	require.Equal(t, "test", string(series.Labels[0].Value))
	require.Equal(t, "Capital", string(series.Labels[1].Name))
	require.Equal(t, "value1", string(series.Labels[1].Value))
}

func TestSeriesCreator_PodMetadata(t *testing.T) {
	sc := seriesCreator{}

	m := &io_prometheus_client.Metric{}
	series := sc.newSeries("test", ScrapeTarget{
		Namespace: "namespace",
		Pod:       "pod",
		Container: "container",
	}, m)
	require.Equal(t, 4, len(series.Labels))
	require.Equal(t, "__name__", string(series.Labels[0].Name))
	require.Equal(t, "test", string(series.Labels[0].Value))
	require.Equal(t, "adxmon_container", string(series.Labels[1].Name))
	require.Equal(t, "container", string(series.Labels[1].Value))
	require.Equal(t, "adxmon_namespace", string(series.Labels[2].Name))
	require.Equal(t, "namespace", string(series.Labels[2].Value))
	require.Equal(t, "adxmon_pod", string(series.Labels[3].Name))
	require.Equal(t, "pod", string(series.Labels[3].Value))
}

func TestSeriesCreator_AddLabels(t *testing.T) {
	sc := seriesCreator{}

	m := &io_prometheus_client.Metric{
		Label: []*io_prometheus_client.LabelPair{
			{
				Name:  strPtr("foo"),
				Value: strPtr("bar"),
			},
		},
	}
	series := sc.newSeries("test", ScrapeTarget{
		Namespace: "namespace",
		Pod:       "pod",
		Container: "container",
	}, m)
	require.Equal(t, 5, len(series.Labels))
	// Labels should be sorted by name
	require.Equal(t, "__name__", string(series.Labels[0].Name))
	require.Equal(t, "test", string(series.Labels[0].Value))
	// adxmon_ should always be added
	require.Equal(t, "adxmon_container", string(series.Labels[1].Name))
	require.Equal(t, "container", string(series.Labels[1].Value))
	require.Equal(t, "adxmon_namespace", string(series.Labels[2].Name))
	require.Equal(t, "namespace", string(series.Labels[2].Value))
	require.Equal(t, "adxmon_pod", string(series.Labels[3].Name))
	require.Equal(t, "pod", string(series.Labels[3].Value))

	// Original label is still present
	require.Equal(t, "foo", string(series.Labels[4].Name))
	require.Equal(t, "bar", string(series.Labels[4].Value))
}

func strPtr(s string) *string {
	return &s
}
