package collector

import (
	"context"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/remote"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

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

func TestScraper_LogOnlyPodHasNoMetricsTargets(t *testing.T) {
	s := NewScraper(&ScraperOpts{})
	p := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "log-only",
			Annotations: map[string]string{
				"adx-mon/log-destination": "Logs:MyApp",
			},
		},
		Spec: v1.PodSpec{Containers: []v1.Container{{
			Name:  "app",
			Ports: []v1.ContainerPort{{ContainerPort: 8080}},
		}}},
		Status: v1.PodStatus{PodIP: "1.2.3.4"},
	}

	s.OnAdd(p, false)
	require.Empty(t, s.Targets())

	p.Annotations["adx-mon/scrape"] = "true"
	s.OnUpdate(nil, p)
	require.Len(t, s.Targets(), 1)
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

	s.targets = make(map[scrapeTargetKey]ScrapeTarget)
	for _, target := range targets {
		s.targets[target.key(p.UID, false)] = target
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
	require.Len(t, targets, 1)
	require.Equal(t, "baz", targets[0].Pod)
}

func TestScraper_ReconcilePodEndpoints(t *testing.T) {
	s := NewScraper(&ScraperOpts{})
	p := fakePod("namespace", "pod", nil, "node")
	p.UID = "pod-uid"
	p.Annotations = map[string]string{"adx-mon/scrape": "true"}
	p.Status.PodIP = "1.2.3.4"
	p.Spec.Containers = []v1.Container{{Name: "app", Ports: []v1.ContainerPort{
		{ContainerPort: 8080}, {ContainerPort: 9090},
	}}}
	static := ScrapeTarget{Addr: "http://localhost:8080/metrics", Namespace: p.Namespace, Pod: p.Name, Container: "app"}
	s.targets[static.key("", true)] = static

	s.OnAdd(p, true)
	require.Len(t, s.Targets(), 3)
	s.OnAdd(p, false)
	require.Len(t, s.Targets(), 3)

	updated := p.DeepCopy()
	updated.Spec.Containers[0].Ports = updated.Spec.Containers[0].Ports[:1]
	s.OnUpdate(p, updated)
	require.Len(t, s.Targets(), 2)
	require.Equal(t, "http://1.2.3.4:8080/metrics", s.Targets()[0].Addr)

	updated.Annotations["adx-mon/scrape"] = "false"
	s.OnUpdate(p, updated)
	require.Equal(t, []ScrapeTarget{static}, s.Targets())

	s.OnAdd(p, false)
	require.Len(t, s.Targets(), 3)
	s.OnDelete(cache.DeletedFinalStateUnknown{Obj: p})
	require.Equal(t, []ScrapeTarget{static}, s.Targets())
}

func TestScraper_DeleteOldPodPreservesReplacement(t *testing.T) {
	s := NewScraper(&ScraperOpts{})
	p := fakePod("namespace", "pod", nil, "node")
	p.UID = "old-uid"
	p.Annotations = map[string]string{"adx-mon/scrape": "true"}
	p.Status.PodIP = "1.2.3.4"
	p.Spec.Containers = []v1.Container{{Name: "app", Ports: []v1.ContainerPort{{ContainerPort: 8080}}}}
	s.OnAdd(p, true)
	replacement := p.DeepCopy()
	replacement.UID = "new-uid"
	s.OnAdd(replacement, false)
	s.OnDelete(p)
	require.Len(t, s.Targets(), 1)
	s.OnDelete(replacement)
	require.Empty(t, s.Targets())
}

func TestMakeTargets_ScrapeInterval(t *testing.T) {
	for _, mode := range []struct {
		name        string
		annotations map[string]string
		count       int
	}{
		{name: "all ports", count: 2},
		{name: "numeric port", annotations: map[string]string{"adx-mon/port": "8080"}, count: 1},
		{name: "named port", annotations: map[string]string{"adx-mon/port": "metrics"}, count: 1},
		{name: "target list", annotations: map[string]string{"adx-mon/targets": "/metrics:metrics,/other:9090"}, count: 2},
	} {
		t.Run(mode.name, func(t *testing.T) {
			for _, tt := range []struct {
				value string
				want  time.Duration
			}{
				{value: ""},
				{value: "30s", want: 30 * time.Second},
				{value: "2m", want: 2 * time.Minute},
				{value: "500ms", want: 500 * time.Millisecond},
				{value: "1.5s", want: 1500 * time.Millisecond},
				{value: "1ns", want: time.Nanosecond},
				{value: "invalid"},
				{value: "30"},
				{value: "0s"},
				{value: "-1s"},
				{value: "999999999999999999999h"},
			} {
				t.Run(tt.value, func(t *testing.T) {
					p := fakePod("namespace", "pod", nil, "node")
					p.Annotations = map[string]string{
						"adx-mon/scrape": "true", "adx-mon/scrape-interval": tt.value,
					}
					for key, value := range mode.annotations {
						p.Annotations[key] = value
					}
					p.Status.PodIP = "1.2.3.4"
					p.Spec.Containers = []v1.Container{{Name: "app", Ports: []v1.ContainerPort{
						{Name: "metrics", ContainerPort: 8080}, {ContainerPort: 9090},
					}}}
					targets := makeTargets(p)
					require.Len(t, targets, mode.count)
					for _, target := range targets {
						require.Equal(t, tt.want, target.ScrapeInterval)
					}
					delete(p.Annotations, "adx-mon/scrape-interval")
					for _, target := range makeTargets(p) {
						require.Zero(t, target.ScrapeInterval)
					}
				})
			}
		})
	}
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
