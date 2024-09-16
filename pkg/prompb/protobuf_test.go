package prompb

import (
	"fmt"
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

func formatBytes(b []byte) string {
	s := ""
	for i, v := range b {
		if i != 0 {
			s += ", "
		}
		s += fmt.Sprint(v)
	}
	return s
}
