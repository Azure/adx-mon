package collector

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/remote"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
)

func TestScraperOpts_RequestTransformer(t *testing.T) {
	opts := &ScraperOpts{
		DefaultDropMetrics: true,
	}

	tr := opts.RequestTransformer()
	require.True(t, tr.DefaultDropMetrics)
}

func TestScraper_sendBatch(t *testing.T) {
	tests := []struct {
		name         string
		writeRequest *prompb.WriteRequest
		opts         *ScraperOpts
	}{
		{
			name:         "TestEmptyWriteRequest",
			writeRequest: &prompb.WriteRequest{},
			opts: &ScraperOpts{
				RemoteClients: []remote.RemoteWriteClient{&fakeClient{expectedSamples: 0}},
			},
		},
		{
			name: "TestValidWriteRequest",
			opts: &ScraperOpts{
				RemoteClients: []remote.RemoteWriteClient{&fakeClient{expectedSamples: 1}},
			},
			writeRequest: &prompb.WriteRequest{
				Timeseries: []*prompb.TimeSeries{
					{
						Labels: []*prompb.Label{
							{Name: []byte("testLabel"), Value: []byte("testValue")},
						},
						Samples: []*prompb.Sample{
							{Value: 1, Timestamp: 123456789},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScraper(tt.opts)
			err := s.sendBatch(context.Background(), tt.writeRequest)
			require.NoError(t, err)
			if tt.opts.RemoteClients[0].(*fakeClient).expectedSamples > 0 {
				require.True(t, tt.opts.RemoteClients[0].(*fakeClient).called)
			}
		})
	}
}

func TestScraper_flushBatchUsesCommonLabels(t *testing.T) {
	client := &capturingClient{}
	s := NewScraper(&ScraperOpts{
		AddLabels: map[string]string{
			"Environment": "prod",
			"Host":        "collector-1",
		},
		DropMetrics:   []*regexp.Regexp{regexp.MustCompile("^drop$")},
		RemoteClients: []remote.RemoteWriteClient{client},
	})
	keep := prompb.TimeSeriesPool.Get()
	keep.AppendLabelString("__name__", "keep")
	keep.AppendLabelString("region", "eastus")
	prompb.Sort(keep.Labels)
	drop := prompb.TimeSeriesPool.Get()
	drop.AppendLabelString("__name__", "drop")
	prompb.Sort(drop.Labels)
	request := &prompb.WriteRequest{Timeseries: []*prompb.TimeSeries{keep, drop}}

	result := s.flushBatch(context.Background(), request)

	require.Equal(t, map[string]string{
		"Environment": "prod",
		"Host":        "collector-1",
	}, client.commonLabels)
	require.Equal(t, []map[string]string{{
		"__name__":    "keep",
		"Environment": "prod",
		"Host":        "collector-1",
		"region":      "eastus",
	}}, client.seriesLabels)
	require.Empty(t, result.Timeseries)
	require.Nil(t, result.CommonLabels)
	require.Nil(t, result.LabelFilter)
	require.Empty(t, keep.Labels)
	require.Empty(t, drop.Labels)
}

func TestScraper_isScrapeable_PodIpReused(t *testing.T) {
	s := &Scraper{
		opts: ScraperOpts{
			NodeName: "node",
		},
	}
	p := fakePod("foo", "bar", nil, "node")
	p.Annotations = map[string]string{
		"adx-mon/scrape": "true",
	}

	p.Status.PodIP = "1.2.3.4"
	p.Spec.Containers = []v1.Container{
		{
			Name: "container",
			Ports: []v1.ContainerPort{
				{
					ContainerPort: 8080,
				},
			},
		},
	}

	targets := s.isScrapeable(p)

	s.targets = make(map[string]ScrapeTarget)
	for _, target := range targets {
		s.targets[target.path()] = target
	}

	// Add a new pod with the same IP and port but different pod name
	p = fakePod("blah", "baz", nil, "node")
	p.Annotations = map[string]string{
		"adx-mon/scrape": "true",
	}

	p.Status.PodIP = "1.2.3.4"
	p.Spec.Containers = []v1.Container{
		{
			Name: "container",
			Ports: []v1.ContainerPort{
				{
					ContainerPort: 8080,
				},
			},
		},
	}

	targets = s.isScrapeable(p)
}

type fakeClient struct {
	expectedSamples int
	called          bool
}

func (f *fakeClient) Write(ctx context.Context, wr *prompb.WriteRequest) error {
	f.called = true
	if len(wr.Timeseries) != f.expectedSamples {
		return fmt.Errorf("expected %d samples, got %d", f.expectedSamples, len(wr.Timeseries))
	}
	return nil
}

func (f *fakeClient) CloseIdleConnections() {}

type capturingClient struct {
	commonLabels map[string]string
	seriesLabels []map[string]string
}

func (c *capturingClient) Write(_ context.Context, wr *prompb.WriteRequest) error {
	c.commonLabels = make(map[string]string, len(wr.CommonLabels))
	for _, label := range wr.CommonLabels {
		c.commonLabels[string(label.Name)] = string(label.Value)
	}
	for _, series := range wr.Timeseries {
		labels := make(map[string]string)
		for label := range wr.Labels(series) {
			labels[string(label.Name)] = string(label.Value)
		}
		c.seriesLabels = append(c.seriesLabels, labels)
	}
	return nil
}

func (c *capturingClient) CloseIdleConnections() {}
