package transform_test

import (
	"bytes"
	"regexp"
	"testing"

	"github.com/Azure/adx-mon/collector/metadata"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/storage"
	"github.com/Azure/adx-mon/transform"
)

var benchmarkSink []byte

// BenchmarkCommonLabelsEndToEnd measures request construction, transformation,
// consumption, and pooled-request cleanup for the two representations.
func BenchmarkCommonLabelsEndToEnd(b *testing.B) {
	for _, consumer := range []string{"protobuf", "csv-segment-key"} {
		for _, representation := range []string{"materialized", "lazy"} {
			b.Run(consumer+"/"+representation, func(b *testing.B) {
				benchmarkCommonLabelsEndToEnd(b, consumer, representation == "lazy")
			})
		}
	}
}

func benchmarkCommonLabelsEndToEnd(b *testing.B, consumer string, lazy bool) {
	b.ReportAllocs()

	// Initialization includes sync.Once work and is deliberately outside the
	// timed request path.
	transformer := newEndToEndTransformer()
	warmRequest := newEndToEndRequest(lazy)
	if lazy {
		transformer.TransformWriteRequestWithCommonLabels(warmRequest)
	} else {
		transformer.TransformWriteRequest(warmRequest)
	}
	prompb.WriteRequestPool.Put(warmRequest)

	protobufBuffer := make([]byte, 0, 128*1024)
	segmentBuffer := make([]byte, 0, 128)
	csvBuffer := bytes.NewBuffer(make([]byte, 0, 256*1024))
	csvWriter := transform.NewMetricsCSVWriter(csvBuffer, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		request := newEndToEndRequest(lazy)
		if lazy {
			transformer.TransformWriteRequestWithCommonLabels(request)
		} else {
			transformer.TransformWriteRequest(request)
		}

		switch consumer {
		case "protobuf":
			var err error
			protobufBuffer, err = request.MarshalTo(protobufBuffer[:0])
			if err != nil {
				b.Fatal(err)
			}
			benchmarkSink = protobufBuffer
		case "csv-segment-key":
			csvWriter.Reset()
			for _, series := range request.Timeseries {
				var err error
				segmentBuffer, err = storage.SegmentKey(segmentBuffer[:0], request, series, uint64(i))
				if err != nil {
					b.Fatal(err)
				}
				if err := csvWriter.MarshalCSV(request, series); err != nil {
					b.Fatal(err)
				}
			}
			benchmarkSink = append(benchmarkSink[:0], segmentBuffer...)
			benchmarkSink = append(benchmarkSink, csvWriter.Bytes()...)
		}

		prompb.WriteRequestPool.Put(request)
	}
}

func newEndToEndTransformer() *transform.RequestTransformer {
	return &transform.RequestTransformer{
		AddLabels: map[string]string{
			"adxmon_database": "Metrics",
			"collector":       "collector-1",
		},
		DynamicLabeler: benchmarkDynamicLabeler{},
		DropLabels: map[*regexp.Regexp]*regexp.Regexp{
			regexp.MustCompile("^cpu$"):    regexp.MustCompile("^(Environment|Host)$"),
			regexp.MustCompile("^memory$"): regexp.MustCompile("^(Cloud|Region)$"),
		},
	}
}

func newEndToEndRequest(lazy bool) *prompb.WriteRequest {
	request := prompb.WriteRequestPool.Get()
	if lazy {
		for _, label := range endToEndCommonLabelSpecs {
			request.CommonLabels = append(request.CommonLabels, &prompb.Label{
				Name:  []byte(label.name),
				Value: []byte(label.value),
			})
		}
	}

	for i := 0; i < 64; i++ {
		name := "cpu"
		if i%2 != 0 {
			name = "memory"
		}
		series := prompb.TimeSeriesPool.Get()
		series.AppendLabelString("__name__", name)
		series.AppendLabelString("instance", "10.0.0.1")
		series.AppendLabelString("job", "node")
		series.AppendLabelString("pod", "pod-17")
		if !lazy {
			for _, label := range endToEndCommonLabelSpecs {
				series.AppendLabelString(label.name, label.value)
			}
		}
		prompb.Sort(series.Labels)
		for sample := 0; sample < 4; sample++ {
			series.Samples = append(series.Samples, &prompb.Sample{
				Timestamp: int64(1700000000000 + i*1000 + sample*100),
				Value:     float64(i) + float64(sample)/10,
			})
		}
		request.Timeseries = append(request.Timeseries, series)
	}
	return request
}

type endToEndLabelSpec struct {
	name  string
	value string
}

var endToEndCommonLabelSpecs = [...]endToEndLabelSpec{
	{name: "Cloud", value: "Public"},
	{name: "Environment", value: "prod"},
	{name: "Host", value: "collector-1"},
	{name: "Region", value: "eastus"},
	{name: "RPTenant", value: "tenant-1"},
	{name: "UnderlayName", value: "underlay-1"},
}

// benchmarkDynamicLabeler is deterministic and intentionally supplies labels
// that are common to every series, as a metadata-backed labeler does.
type benchmarkDynamicLabeler struct{}

var _ metadata.MetricLabeler = benchmarkDynamicLabeler{}

func (benchmarkDynamicLabeler) AppendLabelNamesBytes(names [][]byte) [][]byte {
	return append(names, []byte("cluster"), []byte("zone"))
}

func (benchmarkDynamicLabeler) WalkLabels(callback func(key, value []byte)) {
	callback([]byte("cluster"), []byte("aks-prod"))
	callback([]byte("zone"), []byte("eastus-1"))
}

func (benchmarkDynamicLabeler) AppendPromLabels(series *prompb.TimeSeries) {
	series.AppendLabelString("cluster", "aks-prod")
	series.AppendLabelString("zone", "eastus-1")
}

func TestCommonLabelsEndToEndEquivalent(t *testing.T) {
	materializedTransformer := newEndToEndTransformer()
	lazyTransformer := newEndToEndTransformer()
	materialized := newEndToEndRequest(false)
	lazy := newEndToEndRequest(true)
	materializedTransformer.TransformWriteRequest(materialized)
	lazyTransformer.TransformWriteRequestWithCommonLabels(lazy)
	t.Cleanup(func() {
		prompb.WriteRequestPool.Put(materialized)
		prompb.WriteRequestPool.Put(lazy)
	})

	materializedBytes, err := materialized.MarshalTo(nil)
	if err != nil {
		t.Fatal(err)
	}
	lazyBytes, err := lazy.MarshalTo(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(materializedBytes, lazyBytes) {
		t.Fatal("materialized and lazy protobuf output differ")
	}

	materializedCSV := bytes.NewBuffer(nil)
	lazyCSV := bytes.NewBuffer(nil)
	materializedWriter := transform.NewMetricsCSVWriter(materializedCSV, nil)
	lazyWriter := transform.NewMetricsCSVWriter(lazyCSV, nil)
	for i := range materialized.Timeseries {
		materializedSeries := materialized.Timeseries[i]
		lazySeries := lazy.Timeseries[i]
		materializedKey, err := storage.SegmentKey(nil, materialized, materializedSeries, 42)
		if err != nil {
			t.Fatal(err)
		}
		lazyKey, err := storage.SegmentKey(nil, lazy, lazySeries, 42)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(materializedKey, lazyKey) {
			t.Fatalf("segment key differs for series %d", i)
		}
		if err := materializedWriter.MarshalCSV(materialized, materializedSeries); err != nil {
			t.Fatal(err)
		}
		if err := lazyWriter.MarshalCSV(lazy, lazySeries); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(materializedCSV.Bytes(), lazyCSV.Bytes()) {
		t.Fatal("materialized and lazy CSV output differ")
	}
}
