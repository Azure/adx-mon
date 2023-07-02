package transform_test

import (
	"regexp"
	"testing"

	"github.com/Azure/adx-mon/ingestor/transform"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/stretchr/testify/require"
)

func TestRequestFilter_Filter_DropMetrics(t *testing.T) {
	f := &transform.RequestFilter{DropMetrics: []*regexp.Regexp{
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

	res := f.Filter(req)
	require.Equal(t, 1, len(res.Timeseries))
}

func TestRequestFilter_Filter_DropMetricsRegex(t *testing.T) {
	f := &transform.RequestFilter{DropMetrics: []*regexp.Regexp{
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

	res := f.Filter(req)
	require.Equal(t, 1, len(res.Timeseries))
}

func TestRequestFilter_Filter_DropLabels(t *testing.T) {
	f := &transform.RequestFilter{
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

	res := f.Filter(req)
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

func TestRequestFilter_Filter_SkipNameLabel(t *testing.T) {
	f := &transform.RequestFilter{
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

	res := f.Filter(req)
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
