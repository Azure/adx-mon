package collector

import (
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/prometheus/client_model/go"
)

type seriesCreator struct{}

func (s *seriesCreator) newSeries(name string, scrapeTarget ScrapeTarget, m *io_prometheus_client.Metric) *prompb.TimeSeries {
	ts := prompb.TimeSeriesPool.Get()

	ts.AppendLabelString("__name__", name)
	if scrapeTarget.Namespace != "" {
		ts.AppendLabelString("adxmon_namespace", scrapeTarget.Namespace)
	}
	if scrapeTarget.Pod != "" {
		ts.AppendLabelString("adxmon_pod", scrapeTarget.Pod)
	}
	if scrapeTarget.Container != "" {
		ts.AppendLabelString("adxmon_container", scrapeTarget.Container)
	}

	for _, l := range m.Label {
		if l.GetName() == "adxmon_namespace" || l.GetName() == "adxmon_pod" || l.GetName() == "adxmon_container" || l.GetName() == "adxmon_database" {
			continue
		}

		ts.AppendLabelString(l.GetName(), l.GetValue())
	}

	prompb.Sort(ts.Labels)

	return ts
}
