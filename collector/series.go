package collector

import (
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/prometheus/client_model/go"
)

type seriesCreator struct{}

func (s *seriesCreator) newSeries(name string, scrapeTarget ScrapeTarget, m *io_prometheus_client.Metric) prompb.TimeSeries {
	ts := prompb.TimeSeries{
		Labels: make([]prompb.Label, 0, len(m.Label)+3),
	}

	ts.Labels = append(ts.Labels, prompb.Label{
		Name:  []byte("__name__"),
		Value: []byte(name),
	})

	if scrapeTarget.Namespace != "" {
		ts.Labels = append(ts.Labels, prompb.Label{
			Name:  []byte("adxmon_namespace"),
			Value: []byte(scrapeTarget.Namespace),
		})
	}

	if scrapeTarget.Pod != "" {
		ts.Labels = append(ts.Labels, prompb.Label{
			Name:  []byte("adxmon_pod"),
			Value: []byte(scrapeTarget.Pod),
		})
	}

	if scrapeTarget.Container != "" {
		ts.Labels = append(ts.Labels, prompb.Label{
			Name:  []byte("adxmon_container"),
			Value: []byte(scrapeTarget.Container),
		})
	}

	for _, l := range m.Label {
		if l.GetName() == "adxmon_namespace" || l.GetName() == "adxmon_pod" || l.GetName() == "adxmon_container" {
			continue
		}

		ts.Labels = append(ts.Labels, prompb.Label{
			Name:  []byte(l.GetName()),
			Value: []byte(l.GetValue()),
		})
	}

	prompb.Sort(ts.Labels)

	return ts
}
