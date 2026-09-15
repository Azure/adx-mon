package transform

import (
	"bytes"
	"regexp"
	"testing"

	"github.com/Azure/adx-mon/pkg/prompb"
)

func BenchmarkCommonLabelsProductionConsumers(b *testing.B) {
	lazy, materialized := newConsumerBenchmarkRequests()

	b.Run("protobuf/lazy-filtered-walk", func(b *testing.B) {
		benchmarkMarshalRequest(b, lazy)
	})
	b.Run("protobuf/materialized", func(b *testing.B) {
		benchmarkMarshalRequest(b, materialized)
	})

	b.Run("csv/lazy-filtered-walk", func(b *testing.B) {
		benchmarkCSVRequest(b, lazy)
	})
	b.Run("csv/materialized", func(b *testing.B) {
		benchmarkCSVRequest(b, materialized)
	})
}

func newConsumerBenchmarkRequests() (*prompb.WriteRequest, *prompb.WriteRequest) {
	transformer := &RequestTransformer{DropLabels: map[*regexp.Regexp]*regexp.Regexp{
		regexp.MustCompile("^cpu$"):    regexp.MustCompile("^(Environment|Host)$"),
		regexp.MustCompile("^memory$"): regexp.MustCompile("^(Cloud|Region)$"),
	}}
	commonLabels := []*prompb.Label{
		{Name: []byte("Cloud"), Value: []byte("Public")},
		{Name: []byte("Environment"), Value: []byte("prod")},
		{Name: []byte("Host"), Value: []byte("collector-1")},
		{Name: []byte("Region"), Value: []byte("eastus")},
		{Name: []byte("RPTenant"), Value: []byte("tenant-1")},
		{Name: []byte("UnderlayName"), Value: []byte("underlay-1")},
	}
	prompb.Sort(commonLabels)

	lazy := &prompb.WriteRequest{CommonLabels: commonLabels}
	for i := 0; i < 64; i++ {
		name := "cpu"
		if i%2 == 1 {
			name = "memory"
		}
		labels := []*prompb.Label{
			{Name: []byte("__name__"), Value: []byte(name)},
			{Name: []byte("instance"), Value: []byte("10.0.0.1")},
			{Name: []byte("job"), Value: []byte("node")},
		}
		prompb.Sort(labels)
		lazy.Timeseries = append(lazy.Timeseries, &prompb.TimeSeries{
			Labels:  labels,
			Samples: []*prompb.Sample{{Timestamp: 1, Value: 1}},
		})
	}
	transformer.TransformWriteRequestWithCommonLabels(lazy)

	materialized := &prompb.WriteRequest{}
	for _, timeSeries := range lazy.Timeseries {
		labels := make([]*prompb.Label, 0, len(timeSeries.Labels)+len(commonLabels))
		for label := range lazy.Labels(timeSeries) {
			labels = append(labels, label)
		}
		materialized.Timeseries = append(materialized.Timeseries, &prompb.TimeSeries{
			Labels:  labels,
			Samples: timeSeries.Samples,
		})
	}

	return lazy, materialized
}

func benchmarkMarshalRequest(b *testing.B, request *prompb.WriteRequest) {
	b.Helper()
	b.ReportAllocs()
	buffer := make([]byte, 0, 64*1024)
	b.ResetTimer()
	for range b.N {
		var err error
		buffer, err = request.MarshalTo(buffer[:0])
		if err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkCSVRequest(b *testing.B, request *prompb.WriteRequest) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		buffer := bytes.NewBuffer(make([]byte, 0, 64*1024))
		writer := NewMetricsCSVWriter(buffer, nil)
		for _, timeSeries := range request.Timeseries {
			if err := writer.MarshalCSV(request, timeSeries); err != nil {
				b.Fatal(err)
			}
		}
	}
}
