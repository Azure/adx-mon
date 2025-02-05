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
	for i := 0; i < b.N; i++ {
		wr.Timeseries = nil
		require.NoError(b, wr.Unmarshal(buf))
	}
}
