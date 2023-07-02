package transform

import (
	"bytes"
	"regexp"

	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/prompb"
)

type RequestFilter struct {
	// DropLabels is a map of metric names regexes to label name regexes.  When both match, the label will be dropped.
	DropLabels map[*regexp.Regexp]*regexp.Regexp

	// DropMetrics is a slice of regexes that drops metrics when the metric name matches.  The metric name format
	// should match the Prometheus naming style before the metric is translated to a Kusto table name.
	DropMetrics []*regexp.Regexp
}

func (f *RequestFilter) Filter(req prompb.WriteRequest) prompb.WriteRequest {
	var i int
	for j, v := range req.Timeseries {

		// First skip any metrics that should be dropped.
		var skip bool
		for _, r := range f.DropMetrics {
			if r.Match(v.Labels[0].Value) {
				metrics.MetricsDroppedTotal.WithLabelValues(string(v.Labels[0].Value)).Add(float64(len(v.Samples)))
				skip = true
				break
			}
		}

		if skip {

			continue
		}

		// If labels are configured to be dropped, filter them next.
		if len(f.DropLabels) >= 0 {
			var (
				k         int
				skipLabel bool
			)

			for kk, l := range v.Labels {
				// Never attempt to drop __name__ label as this is required to identify the metric.
				if bytes.Equal(l.Name, []byte("__name__")) {
					v.Labels[k] = v.Labels[kk]
					k++
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
				v.Labels[k] = v.Labels[kk]
				k++
			}
			v.Labels = v.Labels[:k]
			req.Timeseries[i].Labels = v.Labels
		}

		req.Timeseries[i] = req.Timeseries[j]
		i++
	}
	req.Timeseries = req.Timeseries[:i]
	return req
}
