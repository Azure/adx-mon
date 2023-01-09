package transform

import (
	"bytes"
	"github.com/Azure/adx-mon/prompb"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMarshalCSV(t *testing.T) {
	ts := prompb.TimeSeries{
		Labels: []prompb.Label{
			{
				Name:  []byte("__name__"),
				Value: []byte("__redis__"),
			},
			{
				Name:  []byte("measurement"),
				Value: []byte("used_cpu_user_children"),
			},
			{
				Name:  []byte("hostname"),
				Value: []byte("host_1"),
			},
			{
				Name:  []byte("region"),
				Value: []byte("eastus"),
			},
		},

		Samples: []prompb.Sample{
			{
				Timestamp: 1669112524001,
				Value:     0,
			},
			{
				Timestamp: 1669112525002,
				Value:     1,
			},
			{
				Timestamp: 1669112526003,
				Value:     2,
			},
		},
	}

	var b bytes.Buffer
	w := NewCSVWriter(&b)
	err := w.MarshalCSV(ts)
	require.NoError(t, err)
	println(b.String())
}

func TestNormalize(t *testing.T) {
	require.Equal(t, "Redis", string(Normalize([]byte("__redis__"))))
	require.Equal(t, "UsedCpuUserChildren", string(Normalize([]byte("used_cpu_user_children"))))
	require.Equal(t, "Host1", string(Normalize([]byte("host_1"))))
	require.Equal(t, "Region", string(Normalize([]byte("region"))))

}
