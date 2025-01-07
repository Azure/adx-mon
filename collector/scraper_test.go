package collector

import (
	"context"
	"fmt"
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
		{
			name: "TestDefaultDropMetrics",
			opts: &ScraperOpts{
				DefaultDropMetrics: true,
				RemoteClients:      []remote.RemoteWriteClient{&fakeClient{expectedSamples: 0}},
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
			wr := s.flushBatchIfNecessary(context.Background(), tt.writeRequest)
			err := s.sendBatch(context.Background(), wr)
			require.NoError(t, err)
			if tt.opts.RemoteClients[0].(*fakeClient).expectedSamples > 0 {
				require.True(t, tt.opts.RemoteClients[0].(*fakeClient).called)
			}
		})
	}
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
