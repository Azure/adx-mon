package transform

import (
	"bytes"
	"testing"

	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/stretchr/testify/require"
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
	w := NewCSVWriter(&b, nil)
	err := w.MarshalCSV(ts)
	require.NoError(t, err)
	require.Equal(t, `2022-11-22T10:22:04.001Z,2808720101239693515,"{""measurement"":""used_cpu_user_children"",""hostname"":""host_1"",""region"":""eastus""}",0.000000000
2022-11-22T10:22:05.002Z,2808720101239693515,"{""measurement"":""used_cpu_user_children"",""hostname"":""host_1"",""region"":""eastus""}",1.000000000
2022-11-22T10:22:06.003Z,2808720101239693515,"{""measurement"":""used_cpu_user_children"",""hostname"":""host_1"",""region"":""eastus""}",2.000000000
`, b.String())
}

func TestMarshalCSV_LiftLabel(t *testing.T) {
	ts := prompb.TimeSeries{
		Labels: []prompb.Label{
			{
				Name:  []byte("__name__"),
				Value: []byte("__redis__"),
			},
			{
				Name:  []byte("hostname"),
				Value: []byte("host_1"),
			},
			{
				Name:  []byte("measurement"),
				Value: []byte("used_cpu_user_children"),
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
	w := NewCSVWriter(&b, []string{"region", "Hostname", "bar"})

	err := w.MarshalCSV(ts)
	require.NoError(t, err)
	require.Equal(t, `2022-11-22T10:22:04.001Z,7916738835213694439,,host_1,eastus,"{""hostname"":""host_1"",""measurement"":""used_cpu_user_children"",""region"":""eastus""}",0.000000000
2022-11-22T10:22:05.002Z,7916738835213694439,,host_1,eastus,"{""hostname"":""host_1"",""measurement"":""used_cpu_user_children"",""region"":""eastus""}",1.000000000
2022-11-22T10:22:06.003Z,7916738835213694439,,host_1,eastus,"{""hostname"":""host_1"",""measurement"":""used_cpu_user_children"",""region"":""eastus""}",2.000000000
`, b.String())
}

func TestNormalize(t *testing.T) {
	require.Equal(t, "Redis", string(Normalize([]byte("__redis__"))))
	require.Equal(t, "UsedCpuUserChildren", string(Normalize([]byte("used_cpu_user_children"))))
	require.Equal(t, "Host1", string(Normalize([]byte("host_1"))))
	require.Equal(t, "Region", string(Normalize([]byte("region"))))

}
