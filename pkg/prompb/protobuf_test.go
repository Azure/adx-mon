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

	b := make([]byte, wr.Size())
	b, err := wr.MarshalTo(b[:0])
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

func TestTimeSeriesReusesLabelsAndSamples(t *testing.T) {
	ts := &TimeSeries{}
	ts.AppendLabelString("__name__", "cpu")
	ts.AppendSample(1, 1)

	label := ts.Labels[0]
	sample := ts.Samples[0]
	nameBuffer := &ts.Labels[0].Name[:cap(ts.Labels[0].Name)][0]
	valueBuffer := &ts.Labels[0].Value[:cap(ts.Labels[0].Value)][0]

	ts.Reset()
	ts.AppendLabelString("__name__", "mem")
	ts.AppendSample(2, 2)

	require.Same(t, label, ts.Labels[0])
	require.Same(t, sample, ts.Samples[0])
	require.Same(t, nameBuffer, &ts.Labels[0].Name[:cap(ts.Labels[0].Name)][0])
	require.Same(t, valueBuffer, &ts.Labels[0].Value[:cap(ts.Labels[0].Value)][0])
}

func TestWriteRequestResetDropsOversizedStorage(t *testing.T) {
	wr := &WriteRequest{
		Timeseries: make([]*TimeSeries, maxRetainedTimeSeries+1),
	}
	for i := range wr.Timeseries {
		wr.Timeseries[i] = &TimeSeries{}
	}

	wr.Reset()

	require.Nil(t, wr.Timeseries)
}

func TestReleaseTimeSeriesDropsOversizedStorage(t *testing.T) {
	ts := &TimeSeries{
		Labels:  make([]*Label, maxRetainedLabels+1),
		Samples: []*Sample{{Value: 1}},
	}

	ReleaseTimeSeries(ts)

	require.Nil(t, ts.Labels)
	require.Nil(t, ts.Samples)
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

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := make([]byte, wr.Size())
		_, err := wr.MarshalTo(buf[:0])
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
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		require.NoError(b, wr.Unmarshal(buf))
		wr.Reset()
	}
}
