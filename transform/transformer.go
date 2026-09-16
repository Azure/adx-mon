package transform

import (
	"bytes"
	"regexp"
	"sync"

	"github.com/Azure/adx-mon/collector/metadata"
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

	// AddLabels is a map of static label names to label values that will be added to all metrics.
	// AddLabels takes precedence over any existing labels with the same name or any dynamic labels.
	AddLabels map[string]string

	// addLabels is the static slice of the labels to add to each TimeSeries. Dynamic labels are walked/added with DynamicLabeler.
	addLabels []*prompb.Label

	// addLabelsKeys is a slice of the label names in AddLabels as byte slices for efficient comparison.
	addLabelsKeys [][]byte

	// labelFilter is the immutable request-level policy shared by transformed
	// requests. It is built once from DropLabels and addLabelsKeys.
	labelFilter *prompb.LabelFilter

	// AllowedDatabase is a map of database names that are allowed to be written to.
	AllowedDatabase map[string]struct{}

	// DynamicLabeler is an optional labeler that adds dynamic labels from metadata sources.
	DynamicLabeler metadata.MetricLabeler

	initOnce sync.Once
}

func (f *RequestTransformer) init() {
	f.initOnce.Do(func() {
		addLabelsSlice := make([]*prompb.Label, 0, len(f.AddLabels))
		if f.DropLabels == nil {
			f.DropLabels = make(map[*regexp.Regexp]*regexp.Regexp)
		}

		if f.KeepMetricsWithLabelValue == nil {
			f.KeepMetricsWithLabelValue = make(map[*regexp.Regexp]*regexp.Regexp)
		}

		// Precompile the static add labels into the slice format used by TimeSeries.
		// Also build the addLabelsKeys slice for efficient comparison when transforming.
		for k, v := range f.AddLabels {
			addLabelsSlice = append(addLabelsSlice, &prompb.Label{
				Name:  []byte(k),
				Value: []byte(v),
			})
			f.addLabelsKeys = append(f.addLabelsKeys, []byte(k))
		}
		prompb.Sort(addLabelsSlice)
		f.addLabels = addLabelsSlice

		// If a DynamicLabeler is configured, ensure its labels are included in the addLabelsKeys slice.
		if f.DynamicLabeler != nil {
			f.addLabelsKeys = f.DynamicLabeler.AppendLabelNamesBytes(f.addLabelsKeys)
		}

		labelFilter := &prompb.LabelFilter{
			Keep: append([][]byte(nil), f.addLabelsKeys...),
		}
		for metricRegexp, labelRegexp := range f.DropLabels {
			labelFilter.Drop = append(labelFilter.Drop, prompb.LabelDropRule{
				Metric: metricRegexp,
				Label:  labelRegexp,
			})
		}
		if len(labelFilter.Drop) > 0 || len(labelFilter.Keep) > 0 {
			f.labelFilter = labelFilter
		}
	})
}

func (f *RequestTransformer) TransformWriteRequest(req *prompb.WriteRequest) *prompb.WriteRequest {
	f.init()
	var i int
	for j := range req.Timeseries {
		v := req.Timeseries[j]
		// First skip any metrics that should be dropped.
		name := prompb.MetricName(v)

		if f.shouldDropMetricWithCommonLabels(v, nil, name) {
			if metrics.DebugMetricsEnabled {
				metrics.MetricsDroppedTotal.WithLabelValues(string(name)).Add(float64(len(v.Samples)))
			}
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
				if metrics.DebugMetricsEnabled {
					metrics.MetricsDroppedTotal.WithLabelValues(string(name)).Add(float64(len(v.Samples)))
				}
				continue
			}
		}

		req.Timeseries[i] = f.TransformTimeSeries(v)
		i++
	}
	req.Timeseries = req.Timeseries[:i]

	return req
}

// TransformWriteRequestWithCommonLabels transforms req while keeping labels
// shared by every series at the request level. Callers may opt into this path
// once all of their downstream consumers support CommonLabels and LabelFilter.
func (f *RequestTransformer) TransformWriteRequestWithCommonLabels(req *prompb.WriteRequest) *prompb.WriteRequest {
	f.init()
	commonLabels := f.commonLabels(req.CommonLabels)

	var i int
	for _, v := range req.Timeseries {
		name := prompb.MetricName(v)
		if f.shouldDropMetricWithCommonLabels(v, commonLabels, name) {
			if metrics.DebugMetricsEnabled {
				metrics.MetricsDroppedTotal.WithLabelValues(string(name)).Add(float64(len(v.Samples)))
			}
			continue
		}

		if len(f.AllowedDatabase) > 0 {
			var database []byte
			for label := range prompb.MergedLabels(v.Labels, commonLabels) {
				if bytes.Equal(label.Name, []byte("adxmon_database")) {
					database = label.Value
					break
				}
			}
			if _, ok := f.AllowedDatabase[string(database)]; !ok {
				if metrics.DebugMetricsEnabled {
					metrics.MetricsDroppedTotal.WithLabelValues(string(name)).Add(float64(len(v.Samples)))
				}
				continue
			}
		}

		f.filterTimeSeries(v)
		req.Timeseries[i] = v
		i++
	}

	req.Timeseries = req.Timeseries[:i]
	req.CommonLabels = commonLabels
	req.LabelFilter = f.labelFilter
	return req
}

func (f *RequestTransformer) TransformTimeSeries(v *prompb.TimeSeries) *prompb.TimeSeries {
	f.init()
	f.filterTimeSeries(v)
	if f.DynamicLabeler != nil {
		f.DynamicLabeler.AppendPromLabels(v)
	}
	for _, ll := range f.addLabels {
		v.AppendLabel(ll.Name, ll.Value)
	}

	prompb.Sort(v.Labels)

	return v
}

func (f *RequestTransformer) filterTimeSeries(v *prompb.TimeSeries) {
	// If labels are configured to be dropped, filter them next.
	var (
		i         int
		skipLabel bool
	)
	name := prompb.MetricName(v)

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
			if metrReg.Match(name) && labelReg.Match(l.Name) {
				skipLabel = true
				break
			}
		}

		if skipLabel {
			continue
		}

		// Skip any labels that will be overwritten by the add labels.
		for _, al := range f.addLabelsKeys {
			if bytes.Equal(l.Name, al) {
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
}

func (f *RequestTransformer) commonLabels(existing []*prompb.Label) []*prompb.Label {
	if f.DynamicLabeler == nil && len(f.addLabels) == 0 {
		return existing
	}

	labels := make([]*prompb.Label, 0, len(existing)+len(f.addLabelsKeys))
	for _, label := range existing {
		var overwritten bool
		for _, name := range f.addLabelsKeys {
			if bytes.Equal(label.Name, name) {
				overwritten = true
				break
			}
		}
		if !overwritten {
			labels = append(labels, label)
		}
	}

	if f.DynamicLabeler != nil {
		f.DynamicLabeler.WalkLabels(func(name, value []byte) {
			for _, label := range f.addLabels {
				if bytes.Equal(name, label.Name) {
					return
				}
			}
			labels = append(labels, &prompb.Label{
				Name:  bytes.Clone(name),
				Value: bytes.Clone(value),
			})
		})
	}

	labels = append(labels, f.addLabels...)
	prompb.Sort(labels)
	return labels
}

// WalkLabels walks the transformed view of a time series using the request's
// common labels and filtering policy. It is safe to call in parallel if the
// callback does not modify the name and value bytes.
func (f *RequestTransformer) WalkLabels(req *prompb.WriteRequest, v *prompb.TimeSeries, callback func(name []byte, value []byte)) {
	f.init()

	var skipLabel bool
	metricName := prompb.MetricName(v)

	for l := range req.Labels(v) {
		// Never attempt to drop __name__ label as this is required to identify the metric.
		if bytes.Equal(l.Name, []byte("__name__")) {
			callback(l.Name, l.Value)
			continue
		}

		// To drop a label, it has to match the metrics regex and the label regex.
		skipLabel = false
		for metrReg, labelReg := range f.DropLabels {
			if metrReg.Match(metricName) && labelReg.Match(l.Name) {
				skipLabel = true
				break
			}
		}

		if skipLabel {
			continue
		}

		// Skip any labels that will be overwritten by the add labels.
		for _, al := range f.addLabelsKeys {
			if bytes.Equal(l.Name, al) {
				skipLabel = true
				break
			}
		}

		if skipLabel {
			continue
		}

		callback(l.Name, l.Value)
	}
	if f.DynamicLabeler != nil {
		f.DynamicLabeler.WalkLabels(callback)
	}
	for _, ll := range f.addLabels {
		callback(ll.Name, ll.Value)
	}
}

// ShouldDropUntransformedMetric reports whether a raw time series should be
// dropped before request-level label transformation has run.
func (f *RequestTransformer) ShouldDropUntransformedMetric(v *prompb.TimeSeries, name []byte) bool {
	return f.shouldDropMetricWithCommonLabels(v, nil, name)
}

func (f *RequestTransformer) shouldDropMetricWithCommonLabels(v *prompb.TimeSeries, commonLabels []*prompb.Label, name []byte) bool {
	if drop, decided := f.shouldDropMetricByName(name); decided {
		return drop
	}
	for label := range prompb.MergedLabels(v.Labels, commonLabels) {
		if f.shouldKeepMetricWithLabel(label) {
			return false
		}
	}
	return true
}

// ShouldDropMetric reports whether a time series from an already-transformed
// request should be dropped. Label-based keep rules inspect the request's
// effective labels and cannot see labels removed upstream.
func (f *RequestTransformer) ShouldDropMetric(req *prompb.WriteRequest, v *prompb.TimeSeries, name []byte) bool {
	if drop, decided := f.shouldDropMetricByName(name); decided {
		return drop
	}
	for label := range req.Labels(v) {
		if f.shouldKeepMetricWithLabel(label) {
			return false
		}
	}
	return true
}

func (f *RequestTransformer) shouldDropMetricByName(name []byte) (drop, decided bool) {
	if f.DefaultDropMetrics {
		// Explicitly dropped metrics take precedence over explicitly kept metrics.
		for _, r := range f.DropMetrics {
			if r.Match(name) {
				return true, true
			}
		}

		for _, r := range f.KeepMetrics {
			if r.Match(name) {
				return false, true
			}
		}

		if len(f.KeepMetricsWithLabelValue) == 0 {
			return true, true
		}
		return false, false
	}

	for _, r := range f.DropMetrics {
		if r.Match(name) {
			return true, true
		}
	}
	return false, true
}

func (f *RequestTransformer) shouldKeepMetricWithLabel(label *prompb.Label) bool {
	for labelRe, valueRe := range f.KeepMetricsWithLabelValue {
		if labelRe.Match(label.Name) && valueRe.Match(label.Value) {
			return true
		}
	}
	return false
}
