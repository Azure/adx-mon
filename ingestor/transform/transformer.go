package transform

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"sync"

	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/prompb"
)

type RequestTransformer struct {
	// DefaultDropMetrics is a flag that indicates whether metrics should be dropped by default unless they match
	// a keep rule.
	DefaultDropMetrics bool

	// KeepMetrics is a slice of regexes that keeps metrics when the metric name matches.  A metric matching a
	// Keep rule will not be dropped even if it matches a drop rule.
	KeepMetrics []*regexp.Regexp

	// KeepMetricsWithLabelValue is a map of regexes of label names to regexes of label values.  When both match,
	// the metric will be kept.
	KeepMetricsWithLabelValue map[*regexp.Regexp]*regexp.Regexp

	// DropLabels is a map of metric names regexes to label name regexes.  When both match, the label will be dropped.
	DropLabels map[*regexp.Regexp]*regexp.Regexp

	// DropMetrics is a slice of regexes that drops metrics when the metric name matches.  The metric name format
	// should match the Prometheus naming style before the metric is translated to a Kusto table name.
	DropMetrics []*regexp.Regexp

	// AddLabels is a map of label names to label values that will be added to all metrics.
	AddLabels map[string]string

	addLabels []prompb.Label

	// AllowedDatabase is a map of database names that are allowed to be written to.
	AllowedDatabase map[string]struct{}

	initOnce sync.Once
}

func (f *RequestTransformer) init() {
	f.initOnce.Do(func() {
		addLabelsSlice := make([]prompb.Label, 0, len(f.AddLabels))
		if f.DropLabels == nil {
			f.DropLabels = make(map[*regexp.Regexp]*regexp.Regexp)
		}

		if f.KeepMetricsWithLabelValue == nil {
			f.KeepMetricsWithLabelValue = make(map[*regexp.Regexp]*regexp.Regexp)
		}

		for k, v := range f.AddLabels {
			addLabelsSlice = append(addLabelsSlice, prompb.Label{
				Name:  []byte(k),
				Value: []byte(v),
			})
			f.DropLabels[regexp.MustCompile(fmt.Sprintf("^%s\\b", k))] = regexp.MustCompile(".*")
		}
		prompb.Sort(addLabelsSlice)
		f.addLabels = addLabelsSlice
	})
}

func (f *RequestTransformer) TransformWriteRequest(req prompb.WriteRequest) prompb.WriteRequest {
	f.init()
	var i int
	for j := range req.Timeseries {
		v := req.Timeseries[j]
		// First skip any metrics that should be dropped.
		name := prompb.MetricName(v)

		if !f.ShouldKeepTimeSeries(v, name) {
			metrics.MetricsDroppedTotal.WithLabelValues(string(name)).Add(float64(len(v.Samples)))
			continue
		}

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
	f.init()
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
		for _, al := range f.addLabels {
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
		a = append(a, f.addLabels...)
		v.Labels = a
	}

	sort.Sort(prompb.Labels(v.Labels))

	return v
}

func (f *RequestTransformer) ShouldKeepTimeSeries(v prompb.TimeSeries, name []byte) bool {
	if len(f.KeepMetrics) > 0 {
		for _, r := range f.KeepMetrics {
			if r.Match(name) {
				return true
			}
		}
	}

	if len(f.KeepMetricsWithLabelValue) > 0 {
		for _, label := range v.Labels {
			// Keep metrics that have a certain label
			for lableRe, valueRe := range f.KeepMetricsWithLabelValue {
				if lableRe.Match(label.Name) && valueRe.Match(label.Value) {
					return true
				}
			}
		}

		return false
	}

	return !f.DefaultDropMetrics
}

func (f *RequestTransformer) ShouldDropMetric(name []byte) bool {
	for _, r := range f.DropMetrics {
		if r.Match(name) {
			return true
		}
	}
	return false
}
