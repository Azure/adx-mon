package collector

import (
	"context"
	"fmt"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/adx-mon/prompb"
	"github.com/Azure/adx-mon/promremote"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_model/go"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	v12 "k8s.io/client-go/listers/core/v1"
	"net/http"
	_ "net/http/pprof"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type Service struct {
	opts   *ServiceOpts
	K8sCli kubernetes.Interface

	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc

	remoteClient *promremote.Client
	Tags         map[string]string
	watcher      watch.Interface

	mu      sync.RWMutex
	targets []string
	factory informers.SharedInformerFactory
	pl      v12.PodLister
	srv     *http.Server
}

type ServiceOpts struct {
	ListentAddr    string
	K8sCli         kubernetes.Interface
	NodeName       string
	Targets        []string
	Endpoints      []string
	Tags           map[string]string
	ScrapeInterval time.Duration
}

func NewService(opts *ServiceOpts) (*Service, error) {
	return &Service{
		opts:   opts,
		Tags:   opts.Tags,
		K8sCli: opts.K8sCli,
	}, nil
}

func (s *Service) Open(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)

	var err error
	s.remoteClient, err = promremote.NewClient(10 * time.Second)
	if err != nil {
		return fmt.Errorf("failed to create prometheus remote client: %w", err)
	}

	// Add static targets
	for _, target := range s.opts.Targets {
		logger.Info("Adding static target %s", target)
		s.targets = append(s.targets, target)
	}

	factory := informers.NewSharedInformerFactory(s.K8sCli, time.Minute)
	podsInformer := factory.Core().V1().Pods().Informer()

	factory.Start(s.ctx.Done()) // Start processing these informers.
	factory.WaitForCacheSync(s.ctx.Done())
	s.factory = factory

	pl := factory.Core().V1().Pods().Lister()
	s.pl = pl

	if _, err := podsInformer.AddEventHandler(s); err != nil {
		return err
	}

	// Discover the initial targets running on the node
	pods, err := s.K8sCli.CoreV1().Pods("").List(s.ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=" + s.opts.NodeName),
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}
	for _, pod := range pods.Items {
		if pod.Spec.NodeName != s.opts.NodeName {
			continue
		}

		target := makeTarget(&pod)
		if target != "" {
			logger.Info("Adding target %s/%s %s", pod.Namespace, pod.Name, target)
			s.targets = append(s.targets, target)
		}
	}

	logger.Info("Listening at %s", s.opts.ListentAddr)
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	s.srv = &http.Server{Addr: s.opts.ListentAddr, Handler: mux}

	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}()

	go s.scrape()
	return nil
}

func (s *Service) Close() error {
	s.cancel()
	s.srv.Shutdown(s.ctx)
	s.factory.Shutdown()
	s.wg.Wait()
	return nil
}

func (s *Service) scrape() {
	s.wg.Add(1)
	defer s.wg.Done()

	t := time.NewTicker(s.opts.ScrapeInterval)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			s.scrapeTargets()
		}
	}
}

func (s *Service) scrapeTargets() {
	targets := s.Targets()
	for _, target := range targets {
		fams, err := FetchMetrics(target)
		if err != nil {
			logger.Error("Failed to scrape metrics for %s: %s", target, err.Error())
			continue
		}

		wr := &prompb.WriteRequest{}
		for name, val := range fams {
			for _, m := range val.Metric {
				ts := s.newSeries(name, m)

				timestamp := m.GetTimestampMs()
				if timestamp == 0 {
					timestamp = time.Now().UnixNano() / 1e6
				}
				sample := prompb.Sample{
					Timestamp: timestamp,
				}

				if m.GetCounter() != nil {
					sample.Value = m.GetCounter().GetValue()
				} else if m.GetGauge() != nil {
					sample.Value = m.GetGauge().GetValue()
				} else if m.GetUntyped() != nil {
					sample.Value = m.GetUntyped().GetValue()
				} else if m.GetSummary() != nil {
					sum := m.GetSummary()

					// Add the quantile series
					for _, q := range sum.GetQuantile() {
						ts := s.newSeries(name, m)
						ts.Labels = append(ts.Labels, prompb.Label{
							Name:  []byte("quantile"),
							Value: []byte(fmt.Sprintf("%f", q.GetQuantile())),
						})
						ts.Samples = []prompb.Sample{
							{
								Timestamp: timestamp,
								Value:     q.GetValue(),
							},
						}
						wr.Timeseries = append(wr.Timeseries, ts)
					}

					// Add sum series
					ts := s.newSeries(fmt.Sprintf("%s_sum", name), m)
					ts.Samples = []prompb.Sample{
						{
							Timestamp: timestamp,
							Value:     sum.GetSampleSum(),
						},
					}
					wr.Timeseries = append(wr.Timeseries, ts)

					// Add sum series
					ts = s.newSeries(fmt.Sprintf("%s_count", name), m)
					ts.Samples = []prompb.Sample{
						{
							Timestamp: timestamp,
							Value:     float64(sum.GetSampleCount()),
						},
					}
					wr.Timeseries = append(wr.Timeseries, ts)
				} else if m.GetHistogram() != nil {
					hist := m.GetHistogram()
					// Add the quantile series
					for _, q := range hist.GetBucket() {
						ts := s.newSeries(name, m)
						ts.Labels = append(ts.Labels, prompb.Label{
							Name:  []byte("le"),
							Value: []byte(fmt.Sprintf("%f", q.GetUpperBound())),
						})
						ts.Samples = []prompb.Sample{
							{
								Timestamp: timestamp,
								Value:     q.GetCumulativeCountFloat(),
							},
						}
						wr.Timeseries = append(wr.Timeseries, ts)
					}

					// Add sum series
					ts := s.newSeries(fmt.Sprintf("%s_sum", name), m)
					ts.Samples = []prompb.Sample{
						{
							Timestamp: timestamp,
							Value:     hist.GetSampleSum(),
						},
					}
					wr.Timeseries = append(wr.Timeseries, ts)

					// Add sum series
					ts = s.newSeries(fmt.Sprintf("%s_count", name), m)
					ts.Samples = []prompb.Sample{
						{
							Timestamp: timestamp,
							Value:     float64(hist.GetSampleCount()),
						},
					}
					wr.Timeseries = append(wr.Timeseries, ts)
				}

				ts.Samples = append(ts.Samples, sample)
				wr.Timeseries = append(wr.Timeseries, ts)
			}
		}

		if len(s.opts.Endpoints) == 0 {
			var sb strings.Builder
			for _, ts := range wr.Timeseries {
				sb.Reset()
				for i, l := range ts.Labels {
					sb.Write(l.Name)
					sb.WriteString("=")
					sb.Write(l.Value)
					if i < len(ts.Labels)-1 {
						sb.Write([]byte(","))
					}
				}
				sb.Write([]byte(" "))
				for _, s := range ts.Samples {
					logger.Info("%s %d %f", sb.String(), s.Timestamp, s.Value)
				}

			}
		}

		// TODO: Send write requests to separate goroutines
		for _, endpoint := range s.opts.Endpoints {
			if err := s.remoteClient.Write(s.ctx, endpoint, wr); err != nil {
				logger.Error(err.Error())
			}
		}
	}
}

func (s *Service) newSeries(name string, m *io_prometheus_client.Metric) prompb.TimeSeries {
	ts := prompb.TimeSeries{
		Labels: []prompb.Label{
			{
				Name:  []byte("__name__"),
				Value: []byte(name),
			},
		},
	}

	for _, l := range m.Label {
		ts.Labels = append(ts.Labels, prompb.Label{
			Name:  []byte(l.GetName()),
			Value: []byte(l.GetValue()),
		})
	}

	for k, v := range s.Tags {
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

func (s *Service) OnAdd(obj interface{}) {
	p := obj.(*v1.Pod)

	target, exists := s.isTarget(p)

	// Not a scrape-able pod
	if target == "" {
		return
	}

	// We're already scraping this pod, nothing to do
	if exists {
		return
	}

	logger.Info("Adding target %s/%s %s", p.Namespace, p.Name, target)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.targets = append(s.targets, target)
}

func (s *Service) OnUpdate(oldObj, newObj interface{}) {
	s.OnAdd(newObj)
}

func (s *Service) OnDelete(obj interface{}) {
	p := obj.(*v1.Pod)
	target, exists := s.isTarget(p)

	// Not a scrapeable pod
	if target == "" {
		return
	}

	// We're not currently scraping this pod, nothing to do
	if !exists {
		return
	}

	logger.Info("Removing target %s/%s %s", p.Namespace, p.Name, target)
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, v := range s.targets {
		if v == target {
			s.targets = append(s.targets[:i], s.targets[i+1:]...)
			return
		}
	}
}

// isTarget returns the scrape target endpoing and true if the pod is currently a target, false otherwise
func (s *Service) isTarget(p *v1.Pod) (string, bool) {
	// If this pod is not schedule to this node, skip it
	if strings.ToLower(p.Spec.NodeName) != strings.ToLower(s.opts.NodeName) {
		return "", false
	}

	target := makeTarget(p)
	if target == "" {
		return "", false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.targets {
		if v == target {
			return target, true
		}
	}

	return target, false
}

func (s *Service) Targets() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	a := make([]string, len(s.targets))
	copy(a, s.targets)
	return a
}

func makeTarget(p *v1.Pod) string {
	// Skip the pod if it has not opted in to scraping
	if p.Annotations["prometheus.io/scrape"] != "true" {
		return ""
	}
	scheme := "http"
	if p.Annotations["prometheus.io/scheme"] != "" {
		scheme = p.Annotations["prometheus.io/scheme"]
	}

	path := "/metrics"
	if p.Annotations["prometheus.io/path"] != "" {
		path = p.Annotations["prometheus.io/path"]
	}
	port := "80"
	if p.Annotations["prometheus.io/port"] != "" {
		port = p.Annotations["prometheus.io/port"]
	}
	podIP := p.Status.PodIP
	if podIP == "" {
		return ""
	}

	return fmt.Sprintf("%s://%s:%s%s", scheme, podIP, port, path)
}
