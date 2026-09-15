package transform

import (
	"bytes"
	"regexp"
	"testing"

	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/schema"
	"github.com/stretchr/testify/require"
)

func TestMetricsCSVWriter_MarshalCSV(t *testing.T) {
	ts := &prompb.TimeSeries{
		Labels: []*prompb.Label{
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

		Samples: []*prompb.Sample{
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
	w := NewMetricsCSVWriter(&b, nil)
	err := w.MarshalCSV(&prompb.WriteRequest{}, ts)
	require.NoError(t, err)
	require.Equal(t, `Timestamp:datetime,SeriesId:long,Labels:dynamic,Value:real
2022-11-22T10:22:04.001Z,-9070404444212865161,"{""measurement"":""used_cpu_user_children"",""hostname"":""host_1"",""region"":""eastus""}",0.000000000
2022-11-22T10:22:05.002Z,-9070404444212865161,"{""measurement"":""used_cpu_user_children"",""hostname"":""host_1"",""region"":""eastus""}",1.000000000
2022-11-22T10:22:06.003Z,-9070404444212865161,"{""measurement"":""used_cpu_user_children"",""hostname"":""host_1"",""region"":""eastus""}",2.000000000
`, string(w.Bytes()))

}

func TestMetricsCSVWriter_MarshalCSVWithCommonLabels(t *testing.T) {
	seriesLabels := []*prompb.Label{
		{Name: []byte("__name__"), Value: []byte("requests_total")},
		{Name: []byte("hostname"), Value: []byte("host-1")},
		{Name: []byte("region"), Value: []byte("eastus")},
	}
	commonLabels := []*prompb.Label{
		{Name: []byte("adxmon_database"), Value: []byte("Metrics")},
		{Name: []byte("Environment"), Value: []byte("prod")},
		{Name: []byte("measurement"), Value: []byte("requests")},
	}
	materializedLabels := append([]*prompb.Label(nil), seriesLabels...)
	materializedLabels = append(materializedLabels, commonLabels...)
	prompb.Sort(materializedLabels)
	samples := []*prompb.Sample{{Timestamp: 1669112524001, Value: 1}}
	lifted := []Field{
		{Name: "Environment", Source: "Environment", Type: "string"},
		{Name: "Hostname", Source: "hostname", Type: "string"},
		{Name: "Missing", Source: "missing", Type: "string"},
	}

	var materializedBuffer bytes.Buffer
	materializedWriter := NewMetricsCSVWriter(&materializedBuffer, lifted)
	materializedSeries := &prompb.TimeSeries{Labels: materializedLabels, Samples: samples}
	require.NoError(t, materializedWriter.MarshalCSV(&prompb.WriteRequest{}, materializedSeries))

	var commonBuffer bytes.Buffer
	commonWriter := NewMetricsCSVWriter(&commonBuffer, lifted)
	commonSeries := &prompb.TimeSeries{Labels: seriesLabels, Samples: samples}
	commonRequest := &prompb.WriteRequest{CommonLabels: commonLabels}
	require.NoError(t, commonWriter.MarshalCSV(commonRequest, commonSeries))

	require.Equal(t, materializedBuffer.String(), commonBuffer.String())
}

func TestMetricsCSVWriter_MarshalCSVWithCommonLabelsAppliesFilterAndLiftsKeptCommonLabel(t *testing.T) {
	series := &prompb.TimeSeries{
		Labels: []*prompb.Label{
			{Name: []byte("__name__"), Value: []byte("requests_total")},
			{Name: []byte("region"), Value: []byte("eastus")},
		},
		Samples: []*prompb.Sample{{Timestamp: 1669112524001, Value: 1}},
	}
	request := &prompb.WriteRequest{
		CommonLabels: []*prompb.Label{
			{Name: []byte("Environment"), Value: []byte("prod")},
			{Name: []byte("Secret"), Value: []byte("hidden")},
		},
		LabelFilter: &prompb.LabelFilter{Drop: []prompb.LabelDropRule{{
			Metric: regexp.MustCompile("^requests_total$"),
			Label:  regexp.MustCompile("^Secret$"),
		}}},
	}
	materialized := &prompb.TimeSeries{
		Labels: []*prompb.Label{
			{Name: []byte("__name__"), Value: []byte("requests_total")},
			{Name: []byte("Environment"), Value: []byte("prod")},
			{Name: []byte("region"), Value: []byte("eastus")},
		},
		Samples: series.Samples,
	}
	lifted := []Field{
		{Name: "Environment", Source: "Environment", Type: "string"},
		{Name: "Region", Source: "region", Type: "string"},
	}

	var expected bytes.Buffer
	require.NoError(t, NewMetricsCSVWriter(&expected, lifted).MarshalCSV(&prompb.WriteRequest{}, materialized))
	var actual bytes.Buffer
	require.NoError(t, NewMetricsCSVWriter(&actual, lifted).MarshalCSV(request, series))

	require.Equal(t, expected.String(), actual.String())
	require.Contains(t, actual.String(), ",1.000000000,prod,eastus\n", "kept common label should populate its lifted column")
}

func BenchmarkMetricsCSVWriter_MarshalCSV(b *testing.B) {
	ts := &prompb.TimeSeries{
		Labels: []*prompb.Label{
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

		Samples: []*prompb.Sample{
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

	buf := bytes.NewBuffer(make([]byte, 0, 64*1024))
	w := NewMetricsCSVWriter(buf, []Field{
		{Name: "Region", Source: "region", Type: "string"},
		{Name: "Hostname", Source: "hostname", Type: "string"},
		{Name: "Bar", Source: "bar", Type: "string"},
	})
	b.ResetTimer()
	request := &prompb.WriteRequest{}

	for i := 0; i < b.N; i++ {
		w.MarshalCSV(request, ts)
		buf.Reset()
	}
}

func TestMetricsCSVWriter_MarshalCSV_LiftLabel(t *testing.T) {
	ts := &prompb.TimeSeries{
		Labels: []*prompb.Label{
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

		Samples: []*prompb.Sample{
			{
				Timestamp: 1669112524001,
				Value:     0,
			},
		},
	}

	mapping := schema.DefaultMetricsMapping
	mapping = mapping.AddStringMapping("Zip")
	mapping = mapping.AddStringMapping("Zap")
	mapping = mapping.AddStringMapping("Region")
	mapping = mapping.AddStringMapping("Hostname")
	mapping = mapping.AddStringMapping("Bar")

	var b bytes.Buffer
	w := NewMetricsCSVWriterWithSchema(&b, []Field{
		{Name: "Zip", Source: "zip", Type: "string"},
		{Name: "Zap", Source: "zap", Type: "string"},
		{Name: "Region", Source: "region", Type: "string"},
		{Name: "Hostname", Source: "hostname", Type: "string"},
		{Name: "Bar", Source: "bar", Type: "string"},
	}, mapping)

	err := w.MarshalCSV(&prompb.WriteRequest{}, ts)
	require.NoError(t, err)
	require.Equal(t, `Timestamp:datetime,SeriesId:long,Labels:dynamic,Value:real,Zip:string,Zap:string,Region:string,Hostname:string,Bar:string
2022-11-22T10:22:04.001Z,1265838189064375029,"{""measurement"":""used_cpu_user_children""}",0.000000000,,host_1,eastus,,
`, b.String())
}
