package collector

import (
	"github.com/Azure/adx-mon/prompb"
	"github.com/prometheus/client_model/go"
	"sort"
)

type seriesCreator struct {
	AddLabels map[string]string
}

func (s *seriesCreator) newSeries(name string, scrapeTarget scrapeTarget, m *io_prometheus_client.Metric) prompb.TimeSeries {
	ts := prompb.TimeSeries{
		Labels: []prompb.Label{
			{
				Name:  []byte("__name__"),
				Value: []byte(name),
			},
		},
	}

	if scrapeTarget.Namespace != "" {
		ts.Labels = append(ts.Labels, prompb.Label{
			Name:  []byte("namespace"),
			Value: []byte(scrapeTarget.Namespace),
		})
	}

	if scrapeTarget.Pod != "" {
		ts.Labels = append(ts.Labels, prompb.Label{
			Name:  []byte("pod"),
			Value: []byte(scrapeTarget.Pod),
		})
	}

	if scrapeTarget.Container != "" {
		ts.Labels = append(ts.Labels, prompb.Label{
			Name:  []byte("container"),
			Value: []byte(scrapeTarget.Container),
		})
	}

	for _, l := range m.Label {
		// Skip labels that will be overridden by static labels
		if _, ok := s.AddLabels[l.GetName()]; ok {
			continue
		}

		ts.Labels = append(ts.Labels, prompb.Label{
			Name:  []byte(l.GetName()),
			Value: []byte(l.GetValue()),
		})
	}

	for k, v := range s.AddLabels {
		if k == "namespace" || k == "pod" || k == "container" {
			continue
		}

		ts.Labels = append(ts.Labels, prompb.Label{
			Name:  []byte(k),
			Value: []byte(v),
		})
	}
	sort.Slice(ts.Labels, func(i, j int) bool {
		return string(ts.Labels[i].Name) < string(ts.Labels[j].Name)
	})
	return ts
}
