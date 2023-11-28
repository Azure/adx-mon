package transform

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"

	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/prompb"
)

type RequestTransformer struct {
	// DropLabels is a map of metric names regexes to label name regexes.  When both match, the label will be dropped.
	DropLabels map[*regexp.Regexp]*regexp.Regexp

	// DropMetrics is a slice of regexes that drops metrics when the metric name matches.  The metric name format
	// should match the Prometheus naming style before the metric is translated to a Kusto table name.
	DropMetrics []*regexp.Regexp

	// AddLabels is a map of label names to label values that will be added to all metrics.
	AddLabels []prompb.Label

	// AllowedDatabase is a map of database names that are allowed to be written to.
	AllowedDatabase map[string]struct{}
}

func NewRequestTransformer(addLabels map[string]string, dropLabels map[*regexp.Regexp]*regexp.Regexp, dropMetrics []*regexp.Regexp, allowedDatabase map[string]struct{}) *RequestTransformer {
	addLabelsSlice := make([]prompb.Label, 0, len(addLabels))
	if dropLabels == nil {
		dropLabels = make(map[*regexp.Regexp]*regexp.Regexp)
	}
	for k, v := range addLabels {
		addLabelsSlice = append(addLabelsSlice, prompb.Label{
			Name:  []byte(k),
			Value: []byte(v),
		})
		dropLabels[regexp.MustCompile(fmt.Sprintf("^%s\\b", k))] = regexp.MustCompile(".*")
	}
	prompb.Sort(addLabelsSlice)

	return &RequestTransformer{
		DropLabels:      dropLabels,
		DropMetrics:     dropMetrics,
		AddLabels:       addLabelsSlice,
		AllowedDatabase: allowedDatabase,
	}
}

func (f *RequestTransformer) TransformWriteRequest(req prompb.WriteRequest) prompb.WriteRequest {

	if len(f.DropMetrics) == 0 && len(f.DropLabels) == 0 && len(f.AddLabels) == 0 && len(f.AllowedDatabase) == 0 {
		return req
	}

	var i int

	for j := range req.Timeseries {
		v := req.Timeseries[j]
		// First skip any metrics that should be dropped.
		name := prompb.MetricName(v)
		if f.ShouldDropMetric(name) {
			metrics.MetricsDroppedTotal.WithLabelValues(string(name)).Add(float64(len(v.Samples)))
			continue
		}

		if len(f.AllowedDatabase) > 0 {
			var db []byte
			for _, l := range v.Labels {
				if bytes.Equal(l.Name, []byte("adxmon_database")) {
					db = l.Value
					break
				}
			}

			if _, ok := f.AllowedDatabase[string(db)]; !ok {
				metrics.MetricsDroppedTotal.WithLabelValues(string(name)).Add(float64(len(v.Samples)))
				continue
			}
		}

		req.Timeseries[i] = f.TransformTimeSeries(v)
		i++
	}
	req.Timeseries = req.Timeseries[:i]

	return req
}

func (f *RequestTransformer) TransformTimeSeries(v prompb.TimeSeries) prompb.TimeSeries {
	// If labels are configured to be dropped, filter them next.
	var (
		i         int
		skipLabel bool
	)

	for j, l := range v.Labels {
		// Never attempt to drop __name__ label as this is required to identify the metric.
		if bytes.Equal(l.Name, []byte("__name__")) {
			v.Labels[i] = v.Labels[j]
			i++
			continue
		}

		// To drop a label, it has to match the metrics regex and the label regex.
		skipLabel = false
		for metrReg, labelReg := range f.DropLabels {
			if metrReg.Match(v.Labels[0].Value) && labelReg.Match(l.Name) {
				skipLabel = true
				break
			}
		}

		if skipLabel {
			continue
		}

		// Skip any labels that will be overwritten by the add labels.
		for _, al := range f.AddLabels {
			if bytes.Equal(l.Name, al.Name) {
				skipLabel = true
				break
			}
		}

		if skipLabel {
			continue
		}

		v.Labels[i] = v.Labels[j]
		i++
	}
	v.Labels = v.Labels[:i]

	// The RemoteWriteRequest protobuf unmarshalling uses a pool of labels setup as one contiguous array.  Each
	// TimeSeries is assigned a slice of the pool.  If we need to grow this slice, we need to allocate a new slice
	// as growing it implicitly will overwrite the labels of other TimeSeries.
	if len(f.AddLabels) > 0 {
		a := make([]prompb.Label, 0, len(v.Labels)+len(f.AddLabels))
		a = append(a, v.Labels...)
		a = append(a, f.AddLabels...)
		v.Labels = a
	}

	sort.Sort(prompb.Labels(v.Labels))

	return v
}

func (f *RequestTransformer) ShouldDropMetric(name []byte) bool {
	for _, r := range f.DropMetrics {
		if r.Match(name) {
			return true
		}
	}
	return false
}
