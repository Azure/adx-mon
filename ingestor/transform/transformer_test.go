package transform_test

import (
	"regexp"
	"testing"

	"github.com/Azure/adx-mon/ingestor/transform"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/stretchr/testify/require"
)

func TestRequestTransformer_TransformWriteRequest_DropMetrics(t *testing.T) {
	f := &transform.RequestTransformer{DropMetrics: []*regexp.Regexp{
		regexp.MustCompile("cpu"),
	}}

	req := prompb.WriteRequest{
		Timeseries: []prompb.TimeSeries{
			{
				Labels: []prompb.Label{
					{
						Name:  []byte("__name__"),
						Value: []byte("cpu"),
					},
				},
			},
			{
				Labels: []prompb.Label{
					{
						Name:  []byte("__name__"),
						Value: []byte("mem"),
					},
				},
			},
		},
	}

	res := f.TransformWriteRequest(req)
	require.Equal(t, 1, len(res.Timeseries))
}

func TestRequestTransformer_TransformWriteRequest_DropMetricsRegex(t *testing.T) {
	f := &transform.RequestTransformer{DropMetrics: []*regexp.Regexp{
		regexp.MustCompile("cpu|mem"),
	}}

	req := prompb.WriteRequest{
		Timeseries: []prompb.TimeSeries{
			{
				Labels: []prompb.Label{
					{
						Name:  []byte("__name__"),
						Value: []byte("cpu"),
					},
				},
			},
			{
				Labels: []prompb.Label{
					{
						Name:  []byte("__name__"),
						Value: []byte("mem"),
					},
				},
			},
			{
				Labels: []prompb.Label{
					{
						Name:  []byte("__name__"),
						Value: []byte("net"),
					},
				},
			},
		},
	}

	res := f.TransformWriteRequest(req)
	require.Equal(t, 1, len(res.Timeseries))
}

func TestRequestTransformer_TransformWriteRequest_DropLabels(t *testing.T) {
	f := &transform.RequestTransformer{
		DropLabels: map[*regexp.Regexp]*regexp.Regexp{
			regexp.MustCompile("cpu"): regexp.MustCompile("region"),
		},
	}

	req := prompb.WriteRequest{
		Timeseries: []prompb.TimeSeries{
			{
				Labels: []prompb.Label{
					{
						Name:  []byte("__name__"),
						Value: []byte("cpu"),
					},
					{
						Name:  []byte("region"),
						Value: []byte("eastus"),
					},
				},
			},
			{
				Labels: []prompb.Label{
					{
						Name:  []byte("__name__"),
						Value: []byte("mem"),
					},
					{
						Name:  []byte("region"),
						Value: []byte("westus"),
					},
				},
			},
		},
	}

	res := f.TransformWriteRequest(req)
	require.Equal(t, 2, len(res.Timeseries))
	require.Equal(t, 1, len(res.Timeseries[0].Labels))
	require.Equal(t, []byte("__name__"), req.Timeseries[0].Labels[0].Name)
	require.Equal(t, []byte("cpu"), req.Timeseries[0].Labels[0].Value)

	require.Equal(t, 2, len(res.Timeseries[1].Labels))
	require.Equal(t, []byte("__name__"), req.Timeseries[1].Labels[0].Name)
	require.Equal(t, []byte("mem"), req.Timeseries[1].Labels[0].Value)

	require.Equal(t, []byte("region"), req.Timeseries[1].Labels[1].Name)
	require.Equal(t, []byte("westus"), req.Timeseries[1].Labels[1].Value)
}

func TestRequestTransformer_TransformWriteRequest_SkipNameLabel(t *testing.T) {
	f := &transform.RequestTransformer{
		DropLabels: map[*regexp.Regexp]*regexp.Regexp{
			regexp.MustCompile("cpu"): regexp.MustCompile("name"),
		},
	}

	req := prompb.WriteRequest{
		Timeseries: []prompb.TimeSeries{
			{
				Labels: []prompb.Label{
					{
						Name:  []byte("__name__"),
						Value: []byte("cpu"),
					},
					{
						Name:  []byte("region"),
						Value: []byte("eastus"),
					},
				},
			},
			{
				Labels: []prompb.Label{
					{
						Name:  []byte("__name__"),
						Value: []byte("mem"),
					},
					{
						Name:  []byte("region"),
						Value: []byte("westus"),
					},
				},
			},
		},
	}

	res := f.TransformWriteRequest(req)
	require.Equal(t, 2, len(res.Timeseries))
	require.Equal(t, 2, len(res.Timeseries[0].Labels))
	require.Equal(t, []byte("__name__"), req.Timeseries[0].Labels[0].Name)
	require.Equal(t, []byte("cpu"), req.Timeseries[0].Labels[0].Value)

	require.Equal(t, []byte("region"), req.Timeseries[0].Labels[1].Name)
	require.Equal(t, []byte("eastus"), req.Timeseries[0].Labels[1].Value)

	require.Equal(t, 2, len(res.Timeseries[1].Labels))
	require.Equal(t, []byte("__name__"), req.Timeseries[1].Labels[0].Name)
	require.Equal(t, []byte("mem"), req.Timeseries[1].Labels[0].Value)

	require.Equal(t, []byte("region"), req.Timeseries[1].Labels[1].Name)
	require.Equal(t, []byte("westus"), req.Timeseries[1].Labels[1].Value)
}

func TestRequestTransformer_TransformTimeSeries_AddLabels(t *testing.T) {
	f := &transform.RequestTransformer{
		AddLabels: []prompb.Label{
			{
				Name:  []byte("adxmon_namespace"),
				Value: []byte("namespace"),
			},
			{
				Name:  []byte("adxmon_pod"),
				Value: []byte("pod"),
			},
			{
				Name:  []byte("adxmon_container"),
				Value: []byte("container"),
			},
		}}

	ts := prompb.TimeSeries{
		Labels: []prompb.Label{
			{
				Name:  []byte("region"),
				Value: []byte("eastus"),
			},
			{
				Name:  []byte("__name__"),
				Value: []byte("cpu"),
			},
		},
	}

	res := f.TransformTimeSeries(ts)

	require.Equal(t, 5, len(res.Labels))
	require.Equal(t, []byte("__name__"), res.Labels[0].Name)
	require.Equal(t, []byte("cpu"), res.Labels[0].Value)
	require.Equal(t, []byte("adxmon_container"), res.Labels[1].Name)
	require.Equal(t, []byte("container"), res.Labels[1].Value)
	require.Equal(t, []byte("adxmon_namespace"), res.Labels[2].Name)
	require.Equal(t, []byte("namespace"), res.Labels[2].Value)
	require.Equal(t, []byte("adxmon_pod"), res.Labels[3].Name)
	require.Equal(t, []byte("pod"), res.Labels[3].Value)
	require.Equal(t, []byte("region"), res.Labels[4].Name)
	require.Equal(t, []byte("eastus"), res.Labels[4].Value)

}

func TestRequestTransformer_TransformWriteRequest_AllowedDatabases(t *testing.T) {
	f := &transform.RequestTransformer{
		AllowedDatabase: map[string]struct{}{"foo": {}},
	}

	req := prompb.WriteRequest{
		Timeseries: []prompb.TimeSeries{
			{
				Labels: []prompb.Label{
					{
						Name:  []byte("__name__"),
						Value: []byte("cpu"),
					},
				},
			},
			{
				Labels: []prompb.Label{
					{
						Name:  []byte("__name__"),
						Value: []byte("mem"),
					},
					{
						Name:  []byte("adxmon_database"),
						Value: []byte("foo"),
					},
				},
			},
		},
	}

	res := f.TransformWriteRequest(req)
	require.Equal(t, 1, len(res.Timeseries))
}

func BenchmarkRequestTransformer_TransformTimeSeries(b *testing.B) {
	b.ReportAllocs()
	f := transform.NewRequestTransformer(map[string]string{
		"adxmon_namespace": "default",
		"adxmon_pod":       "pod",
		"adxmon_container": "container",
	}, nil, nil, nil)

	ts := prompb.TimeSeries{
		Labels: []prompb.Label{
			{
				Name:  []byte("__name__"),
				Value: []byte("cpu"),
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.TransformTimeSeries(ts)
	}
}
