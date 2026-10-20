package collector

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/remote"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestScraper_PipelineMetrics(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
		result string
	}{
		{name: "success", status: http.StatusOK, body: "cpu 1\n", result: "success"},
		{name: "http error", status: http.StatusInternalServerError, result: "error"},
		{name: "parse error", status: http.StatusOK, body: "cpu not-a-number\n", result: "error"},
		{name: "read error", status: http.StatusOK, body: strings.Repeat("x", 128*1024), result: "error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			metrics.PipelineStepDurationSeconds.Reset()
			t.Cleanup(metrics.PipelineStepDurationSeconds.Reset)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				w.(http.Flusher).Flush()
				time.Sleep(10 * time.Millisecond)
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()

			client, err := NewMetricsClient(ClientOpts{})
			require.NoError(t, err)
			defer client.Close()
			s := NewScraper(&ScraperOpts{MaxBatchSize: 100})
			s.scrapeClient = client
			s.targets["target"] = ScrapeTarget{Addr: server.URL}

			s.scrapeTargets(context.Background())

			for _, result := range []string{"success", "error"} {
				var m dto.Metric
				h := metrics.PipelineStepDurationSeconds.WithLabelValues("collector", "scrape", result)
				require.NoError(t, h.(prometheus.Metric).Write(&m))
				if result == tt.result {
					require.EqualValues(t, 1, m.GetHistogram().GetSampleCount())
					if tt.status == http.StatusOK {
						require.GreaterOrEqual(t, m.GetHistogram().GetSampleSum(), 0.01, "include response body reception, not just headers")
					}
				} else {
					require.Zero(t, m.GetHistogram().GetSampleCount())
				}
			}
		})
	}
}

func TestScraper_OnUpdatePodReplacement(t *testing.T) {
	tests := []struct {
		name        string
		scrape      string
		terminating bool
	}{
		{name: "scrapeable replacement", scrape: "true"},
		{name: "non-scrapeable replacement", scrape: "false"},
		{name: "terminating replacement", scrape: "true", terminating: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScraper(&ScraperOpts{})
			oldPod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "default",
					Name:        "pod",
					UID:         "old-uid",
					Annotations: map[string]string{"adx-mon/scrape": "true"},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{{
						Name:  "container",
						Ports: []v1.ContainerPort{{ContainerPort: 8080}},
					}},
				},
				Status: v1.PodStatus{PodIP: "1.2.3.4"},
			}
			s.OnAdd(oldPod, false)
			require.Len(t, s.Targets(), 1)

			newPod := oldPod.DeepCopy()
			newPod.UID = "new-uid"
			newPod.Status.PodIP = "1.2.3.5"
			newPod.Annotations["adx-mon/scrape"] = tt.scrape
			if tt.terminating {
				newPod.DeletionTimestamp = new(metav1.Now())
			}

			s.OnUpdate(oldPod, newPod)

			if tt.scrape == "true" && !tt.terminating {
				require.Equal(t, []ScrapeTarget{{
					Addr:      "http://1.2.3.5:8080/metrics",
					Namespace: "default",
					Pod:       "pod",
					Container: "container",
				}}, s.Targets())
			} else {
				require.Empty(t, s.Targets())
			}

			s.OnDelete(newPod)
			require.Empty(t, s.Targets())
		})
	}
}

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
