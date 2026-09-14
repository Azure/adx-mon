package prompb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarshal(t *testing.T) {
	wr := WriteRequest{
		Timeseries: []*TimeSeries{
			{
				Labels: []*Label{
					{
						Name:  []byte("__name__"),
						Value: []byte("cpu")},
				},
				Samples: []*Sample{
					{
						Timestamp: int64(1),
						Value:     1.0,
					},
				},
			},
		},
	}

	b, err := wr.Marshal()
	require.NoError(t, err)
	require.Equal(t, b, []byte{10, 30, 10, 15, 10, 8, 95, 95, 110, 97, 109, 101, 95, 95, 18, 3, 99, 112, 117, 18, 11, 9, 0, 0, 0, 0, 0, 0, 240, 63, 16, 1})
}

func TestMarshalTo(t *testing.T) {
	wr := WriteRequest{
		Timeseries: []*TimeSeries{
			{
				Labels: []*Label{
					{
						Name:  []byte("__name__"),
						Value: []byte("cpu")},
				},
				Samples: []*Sample{
					{
						Timestamp: int64(1),
						Value:     1.0,
					},
				},
			},
		},
	}

	b := make([]byte, 4)
	b, err := wr.MarshalTo(b[:0])
	require.NoError(t, err)
	require.Equal(t, b, []byte{10, 30, 10, 15, 10, 8, 95, 95, 110, 97, 109, 101, 95, 95, 18, 3, 99, 112, 117, 18, 11, 9, 0, 0, 0, 0, 0, 0, 240, 63, 16, 1})
}

func TestUnmarshal(t *testing.T) {
	b := []byte{10, 30, 10, 15, 10, 8, 95, 95, 110, 97, 109, 101, 95, 95, 18, 3, 99, 112, 117, 18, 11, 9, 0, 0, 0, 0, 0, 0, 240, 63, 16, 1}
	wr := WriteRequest{}
	err := wr.Unmarshal(b)
	require.NoError(t, err)
	require.Equal(t, 1, len(wr.Timeseries))
	require.Equal(t, 1, len(wr.Timeseries[0].Labels))
	require.Equal(t, 1, len(wr.Timeseries[0].Samples))
	require.Equal(t, "__name__", string(wr.Timeseries[0].Labels[0].Name))
	require.Equal(t, "cpu", string(wr.Timeseries[0].Labels[0].Value))
	require.Equal(t, int64(1), wr.Timeseries[0].Samples[0].Timestamp)
	require.Equal(t, 1.0, wr.Timeseries[0].Samples[0].Value)
}

func TestMarshalCommonLabels(t *testing.T) {
	seriesLabels := []*Label{
		{Name: []byte("__name__"), Value: []byte("cpu")},
		{Name: []byte("region"), Value: []byte("eastus")},
	}
	commonLabels := []*Label{
		{Name: []byte("adxmon_database"), Value: []byte("Metrics")},
		{Name: []byte("Host"), Value: []byte("collector-1")},
	}
	samples := []*Sample{{Timestamp: 1, Value: 1}}

	withCommonLabels := &WriteRequest{
		Timeseries:   []*TimeSeries{{Labels: seriesLabels, Samples: samples}},
		CommonLabels: commonLabels,
	}
	materializedLabels := append([]*Label(nil), seriesLabels...)
	materializedLabels = append(materializedLabels, commonLabels...)
	Sort(materializedLabels)
	materialized := &WriteRequest{
		Timeseries: []*TimeSeries{{Labels: materializedLabels, Samples: samples}},
	}

	commonBytes, err := withCommonLabels.Marshal()
	require.NoError(t, err)
	materializedBytes, err := materialized.Marshal()
	require.NoError(t, err)
	require.Equal(t, materializedBytes, commonBytes)

	var decoded WriteRequest
	require.NoError(t, decoded.Unmarshal(commonBytes))
	require.Empty(t, decoded.CommonLabels)
	require.Len(t, decoded.Timeseries, 1)
	require.Equal(t, []string{"__name__", "adxmon_database", "Host", "region"}, labelNames(decoded.Timeseries[0].Labels))
}

func TestMarshalCommonLabelsSeriesValueTakesPrecedence(t *testing.T) {
	request := &WriteRequest{
		Timeseries: []*TimeSeries{{Labels: []*Label{
			{Name: []byte("__name__"), Value: []byte("cpu")},
			{Name: []byte("region"), Value: []byte("series-region")},
		}}},
		CommonLabels: []*Label{{Name: []byte("region"), Value: []byte("common-region")}},
	}

	encoded, err := request.Marshal()
	require.NoError(t, err)

	var decoded WriteRequest
	require.NoError(t, decoded.Unmarshal(encoded))
	require.Equal(t, []string{"__name__", "region"}, labelNames(decoded.Timeseries[0].Labels))
	require.Equal(t, "series-region", string(decoded.Timeseries[0].Labels[1].Value))
}

func TestUnmarshalSortsLabels(t *testing.T) {
	request := &WriteRequest{Timeseries: []*TimeSeries{{Labels: []*Label{
		{Name: []byte("region"), Value: []byte("eastus")},
		{Name: []byte("__name__"), Value: []byte("cpu")},
	}}}}
	encoded, err := request.Marshal()
	require.NoError(t, err)

	var decoded WriteRequest
	require.NoError(t, decoded.Unmarshal(encoded))
	require.True(t, IsSorted(decoded.Timeseries[0].Labels))
	require.Equal(t, []string{"__name__", "region"}, labelNames(decoded.Timeseries[0].Labels))
}

func TestTimeSeriesResetDoesNotMutateSharedLabels(t *testing.T) {
	shared := &Label{Name: []byte("Environment"), Value: []byte("prod")}
	series := &TimeSeries{Labels: []*Label{shared}}

	series.Reset()

	require.Equal(t, "Environment", string(shared.Name))
	require.Equal(t, "prod", string(shared.Value))
	require.Empty(t, series.Labels)
}

func TestWriteRequestResetClearsCommonLabels(t *testing.T) {
	request := &WriteRequest{CommonLabels: []*Label{{Name: []byte("Environment"), Value: []byte("prod")}}}

	request.Reset()

	require.Nil(t, request.CommonLabels)
}

func BenchmarkWriteRequestMarshalTo(b *testing.B) {
	wr := WriteRequest{
		Timeseries: []*TimeSeries{
			{
				Labels: []*Label{
					{Name: []byte("__name__"), Value: []byte("cpu")},
					{Name: []byte("instance"), Value: []byte("localhost:9090")},
					{Name: []byte("job"), Value: []byte("prometheus")},
					{Name: []byte("region"), Value: []byte("us-west")},
					{Name: []byte("zone"), Value: []byte("us-west-1a")},
					{Name: []byte("environment"), Value: []byte("production")},
				},
				Samples: []*Sample{
					{Timestamp: int64(1), Value: 1.0},
				},
			},
		},
	}

	buf := make([]byte, 0, 32*1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		buf, err = wr.MarshalTo(buf[:0])
		if err != nil {
			b.Fatalf("MarshalTo failed: %v", err)
		}
	}
}

func BenchmarkWriteRequestUnmarshal(b *testing.B) {
	wr := &WriteRequest{
		Timeseries: []*TimeSeries{
			{
				Labels: []*Label{
					{Name: []byte("__name__"), Value: []byte("cpu")},
					{Name: []byte("instance"), Value: []byte("localhost:9090")},
					{Name: []byte("job"), Value: []byte("prometheus")},
					{Name: []byte("region"), Value: []byte("us-west")},
					{Name: []byte("zone"), Value: []byte("us-west-1a")},
					{Name: []byte("environment"), Value: []byte("production")},
				},
				Samples: []*Sample{
					{Timestamp: int64(1), Value: 1.0},
				},
			},
		},
	}

	buf, err := wr.Marshal()
	require.NoError(b, err)

	b.ResetTimer()

	wr = &WriteRequest{}
	for i := 0; i < b.N; i++ {
		wr.Timeseries = nil
		require.NoError(b, wr.Unmarshal(buf))
	}
}

func BenchmarkWriteRequestMarshalToCommonLabels(b *testing.B) {
	seriesLabels := []*Label{
		newMergeTestLabel("__name__", "http_requests_total"),
		newMergeTestLabel("agentpool", "systempool"),
		newMergeTestLabel("cluster", "cluster-1"),
		newMergeTestLabel("code", "200"),
		newMergeTestLabel("container", "api"),
		newMergeTestLabel("instance", "10.0.0.1:8080"),
		newMergeTestLabel("job", "apiserver"),
		newMergeTestLabel("method", "GET"),
		newMergeTestLabel("namespace", "default"),
		newMergeTestLabel("pod", "api-12345"),
		newMergeTestLabel("region", "eastus2"),
		newMergeTestLabel("status", "success"),
	}
	commonLabels := []*Label{
		newMergeTestLabel("adxmon_container", "collector"),
		newMergeTestLabel("adxmon_database", "AKSCCPMetrics"),
		newMergeTestLabel("adxmon_namespace", "default"),
		newMergeTestLabel("adxmon_pod", "collector-12345"),
		newMergeTestLabel("Cloud", "Public"),
		newMergeTestLabel("Environment", "prod"),
		newMergeTestLabel("Host", "collector-12345"),
		newMergeTestLabel("RPTenant", "tenant-1"),
		newMergeTestLabel("UnderlayName", "underlay-1"),
	}
	samples := []*Sample{{Timestamp: 1, Value: 1}}
	materializedLabels := append([]*Label(nil), seriesLabels...)
	materializedLabels = append(materializedLabels, commonLabels...)
	Sort(materializedLabels)

	b.Run("materialized", func(b *testing.B) {
		benchmarkWriteRequestMarshalTo(b, &WriteRequest{
			Timeseries: []*TimeSeries{{Labels: materializedLabels, Samples: samples}},
		})
	})
	b.Run("common", func(b *testing.B) {
		benchmarkWriteRequestMarshalTo(b, &WriteRequest{
			Timeseries:   []*TimeSeries{{Labels: seriesLabels, Samples: samples}},
			CommonLabels: commonLabels,
		})
	})
}

func benchmarkWriteRequestMarshalTo(b *testing.B, request *WriteRequest) {
	b.Helper()
	b.ReportAllocs()
	buf := make([]byte, 0, 32*1024)
	b.ResetTimer()

	for range b.N {
		var err error
		buf, err = request.MarshalTo(buf[:0])
		if err != nil {
			b.Fatal(err)
		}
	}
}

func labelNames(labels []*Label) []string {
	names := make([]string, len(labels))
	for i, label := range labels {
		names[i] = string(label.Name)
	}
	return names
}
