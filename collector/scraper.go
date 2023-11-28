package collector

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/ingestor/transform"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/promremote"
	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	v12 "k8s.io/client-go/listers/core/v1"

	v1 "k8s.io/api/core/v1"
)

type ScraperOpts struct {
	NodeName string

	AddLabels map[string]string
	// DropLabels is a map of metric names regexes to label name regexes.  When both match, the label will be dropped.
	DropLabels map[*regexp.Regexp]*regexp.Regexp

	// DropMetrics is a slice of regexes that drops metrics when the metric name matches.  The metric name format
	// should match the Prometheus naming style before the metric is translated to a Kusto table name.
	DropMetrics []*regexp.Regexp

	ScrapeInterval time.Duration

	// DisableMetricsForwarding disables the forwarding of metrics to the remote write endpoint.
	DisableMetricsForwarding bool

	K8sCli kubernetes.Interface

	Targets []ScrapeTarget

	// MaxBatchSize is the maximum number of samples to send in a single batch.
	MaxBatchSize int

	Endpoints    []string
	RemoteClient *promremote.Client
}

func (s *ScraperOpts) RequestTransformer() *transform.RequestTransformer {
	return transform.NewRequestTransformer(s.AddLabels, s.DropLabels, s.DropMetrics)
}

type ScrapeTarget struct {
	Addr      string
	Namespace string
	Pod       string
	Container string
}

func (t ScrapeTarget) path() string {
	path := fmt.Sprintf("%s/%s", t.Namespace, t.Pod)
	if t.Container != "" {
		path = fmt.Sprintf("%s/%s", path, t.Container)
	}
	return path
}

func (t ScrapeTarget) String() string {
	return fmt.Sprintf("%s => %s/%s/%s", t.Addr, t.Namespace, t.Pod, t.Container)
}

type Scraper struct {
	K8sCli kubernetes.Interface
	opts   ScraperOpts

	requestTransformer *transform.RequestTransformer
	remoteClient       *promremote.Client
	scrapeClient       *MetricsClient
	watcher            watch.Interface
	seriesCreator      *seriesCreator

	wg     sync.WaitGroup
	cancel context.CancelFunc

	mu      sync.RWMutex
	targets []ScrapeTarget
	factory informers.SharedInformerFactory
	pl      v12.PodLister
}

func NewScraper(opts *ScraperOpts) *Scraper {
	return &Scraper{
		K8sCli:             opts.K8sCli,
		opts:               *opts,
		seriesCreator:      &seriesCreator{},
		requestTransformer: opts.RequestTransformer(),
		remoteClient:       opts.RemoteClient,
	}
}

func (s *Scraper) Open(ctx context.Context) error {
	logger.Infof("Starting prometheus scraper for node %s", s.opts.NodeName)
	ctx, cancelFn := context.WithCancel(ctx)
	s.cancel = cancelFn

	var err error
	s.scrapeClient, err = NewMetricsClient()
	if err != nil {
		return fmt.Errorf("failed to create metrics client: %w", err)
	}

	// Add static targets
	for _, target := range s.opts.Targets {
		logger.Infof("Adding static target %s", target)
		s.targets = append(s.targets, target)
	}

	// Discover the initial targets running on the node
	pods, err := s.K8sCli.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=" + s.opts.NodeName),
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}
	for _, pod := range pods.Items {
		if pod.Spec.NodeName != s.opts.NodeName {
			continue
		}

		targets := makeTargets(&pod)
		for _, target := range targets {
			logger.Infof("Adding target %s %s", target.path(), target)
			s.targets = append(s.targets, target)
		}
	}

	factory := informers.NewSharedInformerFactory(s.K8sCli, time.Minute)
	podsInformer := factory.Core().V1().Pods().Informer()

	factory.Start(ctx.Done()) // Start processing these informers.
	factory.WaitForCacheSync(ctx.Done())
	s.factory = factory

	pl := factory.Core().V1().Pods().Lister()
	s.pl = pl

	if _, err := podsInformer.AddEventHandler(s); err != nil {
		return err
	}

	s.wg.Add(1)
	go s.scrape(ctx)

	return nil
}

func (s *Scraper) Close() error {
	s.scrapeClient.Close()
	s.cancel()
	s.factory.Shutdown()
	s.wg.Wait()

	return nil
}

func (s *Scraper) scrape(ctx context.Context) {
	defer s.wg.Done()

	reconnectTimer := time.NewTicker(5 * time.Minute)
	defer reconnectTimer.Stop()
	t := time.NewTicker(s.opts.ScrapeInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.scrapeTargets(ctx)
		case <-reconnectTimer.C:
			s.remoteClient.CloseIdleConnections()
		}
	}
}

func (s *Scraper) scrapeTargets(ctx context.Context) {
	targets := s.Targets()

	wr := &prompb.WriteRequest{}
	for _, target := range targets {
		fams, err := s.scrapeClient.FetchMetrics(target.Addr)
		if err != nil {
			logger.Errorf("Failed to scrape metrics for %s: %s", target, err.Error())
			continue
		}

		for name, val := range fams {
			// Drop metrics that are in the drop list
			if s.requestTransformer.ShouldDropMetric([]byte(name)) {
				metrics.MetricsDroppedTotal.WithLabelValues(name).Add(float64(len(val.Metric)))
				continue

			}

			for _, m := range val.Metric {
				ts := s.seriesCreator.newSeries(name, target, m)

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
						ts = s.seriesCreator.newSeries(name, target, m)
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

						ts = s.requestTransformer.TransformTimeSeries(ts)
						wr.Timeseries = append(wr.Timeseries, ts)
						wr = s.flushBatchIfNecessary(ctx, wr)
					}

					// Add sum series
					ts := s.seriesCreator.newSeries(fmt.Sprintf("%s_sum", name), target, m)
					ts.Samples = []prompb.Sample{
						{
							Timestamp: timestamp,
							Value:     sum.GetSampleSum(),
						},
					}

					ts = s.requestTransformer.TransformTimeSeries(ts)
					wr.Timeseries = append(wr.Timeseries, ts)
					wr = s.flushBatchIfNecessary(ctx, wr)

					// Add sum series
					ts = s.seriesCreator.newSeries(fmt.Sprintf("%s_count", name), target, m)
					ts.Samples = []prompb.Sample{
						{
							Timestamp: timestamp,
							Value:     float64(sum.GetSampleCount()),
						},
					}

					ts = s.requestTransformer.TransformTimeSeries(ts)
					wr.Timeseries = append(wr.Timeseries, ts)
					wr = s.flushBatchIfNecessary(ctx, wr)
				} else if m.GetHistogram() != nil {
					hist := m.GetHistogram()

					// Add the quantile series
					for _, q := range hist.GetBucket() {
						ts = s.seriesCreator.newSeries(fmt.Sprintf("%s_bucket", name), target, m)
						ts.Labels = append(ts.Labels, prompb.Label{
							Name:  []byte("le"),
							Value: []byte(fmt.Sprintf("%f", q.GetUpperBound())),
						})

						ts.Samples = []prompb.Sample{
							{
								Timestamp: timestamp,
								Value:     float64(q.GetCumulativeCount()),
							},
						}
						ts = s.requestTransformer.TransformTimeSeries(ts)
						wr.Timeseries = append(wr.Timeseries, ts)

						wr = s.flushBatchIfNecessary(ctx, wr)
					}

					// Add sum series
					ts = s.seriesCreator.newSeries(fmt.Sprintf("%s_sum", name), target, m)
					ts.Samples = []prompb.Sample{
						{
							Timestamp: timestamp,
							Value:     hist.GetSampleSum(),
						},
					}

					ts = s.requestTransformer.TransformTimeSeries(ts)
					wr.Timeseries = append(wr.Timeseries, ts)
					wr = s.flushBatchIfNecessary(ctx, wr)

					// Add sum series
					ts = s.seriesCreator.newSeries(fmt.Sprintf("%s_count", name), target, m)
					ts.Samples = []prompb.Sample{
						{
							Timestamp: timestamp,
							Value:     float64(hist.GetSampleCount()),
						},
					}
					ts = s.requestTransformer.TransformTimeSeries(ts)
					wr.Timeseries = append(wr.Timeseries, ts)

					wr = s.flushBatchIfNecessary(ctx, wr)
				}

				ts.Samples = append(ts.Samples, sample)

				ts = s.requestTransformer.TransformTimeSeries(ts)
				wr.Timeseries = append(wr.Timeseries, ts)

				wr = s.flushBatchIfNecessary(ctx, wr)
			}
			wr = s.flushBatchIfNecessary(ctx, wr)
		}
		wr = s.flushBatchIfNecessary(ctx, wr)
	}
	if err := s.sendBatch(ctx, wr); err != nil {
		logger.Errorf(err.Error())
	}
	wr.Timeseries = wr.Timeseries[:0]
}

func (s *Scraper) flushBatchIfNecessary(ctx context.Context, wr *prompb.WriteRequest) *prompb.WriteRequest {
	if len(wr.Timeseries) >= s.opts.MaxBatchSize {
		if err := s.sendBatch(ctx, wr); err != nil {
			logger.Errorf(err.Error())
		}
		wr.Timeseries = wr.Timeseries[:0]
	}
	return wr
}

func (s *Scraper) sendBatch(ctx context.Context, wr *prompb.WriteRequest) error {
	if len(wr.Timeseries) == 0 {
		return nil
	}

	if len(s.opts.Endpoints) == 0 || logger.IsDebug() {
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
				logger.Debugf("%s %d %f", sb.String(), s.Timestamp, s.Value)
			}

		}
	}

	logger.Infof("Sending %d timeseries to %d endpoints", len(wr.Timeseries), len(s.opts.Endpoints))
	g, gCtx := errgroup.WithContext(ctx)
	for _, endpoint := range s.opts.Endpoints {
		endpoint := endpoint
		g.Go(func() error {
			return s.remoteClient.Write(gCtx, endpoint, wr)
		})
	}
	return g.Wait()
}

func (s *Scraper) OnAdd(obj interface{}) {
	p, ok := obj.(*v1.Pod)
	if !ok || p == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	targets, exists := s.isScrapeable(p)

	// Not a scrape-able pod
	if len(targets) == 0 {
		return
	}

	// We're already scraping this pod, nothing to do
	if exists {
		return
	}

	for _, target := range targets {
		logger.Infof("Adding target %s %s", target.path(), target)
		s.targets = append(s.targets, target)
	}
}

func (s *Scraper) OnUpdate(oldObj, newObj interface{}) {
	p, ok := newObj.(*v1.Pod)
	if !ok || p == nil {
		return
	}

	if p.DeletionTimestamp != nil {
		s.OnDelete(p)
	} else {
		s.OnAdd(p)
	}
}

func (s *Scraper) OnDelete(obj interface{}) {
	p, ok := obj.(*v1.Pod)
	if !ok || p == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	targets, exists := s.isScrapeable(p)

	// Not a scrapeable pod
	if len(targets) == 0 {
		return
	}

	// We're not currently scraping this pod, nothing to do
	if !exists {
		return
	}

	var remainingTargets []ScrapeTarget
	for _, target := range targets {
		logger.Infof("Removing target %s %s", target.path(), target)
		for _, v := range s.targets {
			if v.Addr == target.Addr {
				continue
			}
			remainingTargets = append(remainingTargets, v)
		}
	}
	s.targets = remainingTargets
}

// isScrapeable returns the scrape target endpoints and true if the pod is currently a target, false otherwise
func (s *Scraper) isScrapeable(p *v1.Pod) ([]ScrapeTarget, bool) {
	// If this pod is not schedule to this node, skip it
	if strings.ToLower(p.Spec.NodeName) != strings.ToLower(s.opts.NodeName) {
		return nil, false
	}

	targets := makeTargets(p)
	if len(targets) == 0 {
		return nil, false
	}

	// See if any of the pods targets are already being scraped
	for _, v := range s.targets {
		for _, target := range targets {
			if v.Addr == target.Addr {
				return targets, true
			}
		}
	}

	// Not scraping this pod, return all the targets
	return targets, false
}

func (s *Scraper) Targets() []ScrapeTarget {
	s.mu.RLock()
	defer s.mu.RUnlock()

	a := make([]ScrapeTarget, len(s.targets))
	for i, v := range s.targets {
		a[i] = v
	}
	return a
}

func makeTargets(p *v1.Pod) []ScrapeTarget {
	var targets []ScrapeTarget

	// Skip the pod if it has not opted in to scraping
	if !strings.EqualFold(getAnnotationOrDefault(p, "adx-mon/scrape", "false"), "true") {
		return nil
	}

	podIP := p.Status.PodIP
	if podIP == "" {
		return nil
	}

	scheme := getAnnotationOrDefault(p, "adx-mon/scheme", "http")
	path := getAnnotationOrDefault(p, "adx-mon/path", "/metrics")

	// Just scrape this one port
	port := getAnnotationOrDefault(p, "adx-mon/port", "")

	// Scrape a comma separated list of targets with the format path:port like /metrics:8080
	targetMap := getTargetAnnotationMapOrDefault(p, "adx-mon/targets", make(map[string]string))

	for _, c := range p.Spec.Containers {
		for _, cp := range c.Ports {
			var readinessPort, livenessPort string
			if c.ReadinessProbe != nil && c.ReadinessProbe.HTTPGet != nil {
				readinessPort = c.ReadinessProbe.HTTPGet.Port.String()
			}

			if c.LivenessProbe != nil && c.LivenessProbe.HTTPGet != nil {
				livenessPort = c.LivenessProbe.HTTPGet.Port.String()
			}

			// If target list is specified, only scrape those path/port combinations
			if len(targetMap) != 0 {
				checkPorts := []string{strconv.Itoa(int(cp.ContainerPort)), readinessPort, livenessPort}
				for _, checkPort := range checkPorts {
					// if the current port, liveness port, or readiness port exist in the targetMap, add that to scrape targets
					if target, added := addTargetFromMap(podIP, scheme, checkPort, p.Namespace, p.Name, c.Name, targetMap); added {
						targets = append(targets, target)
						// if all targets are accounted for, return target list
						if len(targetMap) == 0 {
							return targets
						}
					}
				}
				// if there are remaining targets, continue iterating through containers
				continue
			}

			// If a port is specified, only scrape that port on the pod
			if port != "" {
				if port != strconv.Itoa(int(cp.ContainerPort)) && port != readinessPort && port != livenessPort {
					continue
				}
				targets = append(targets,
					ScrapeTarget{
						Addr:      fmt.Sprintf("%s://%s:%s%s", scheme, podIP, port, path),
						Namespace: p.Namespace,
						Pod:       p.Name,
						Container: c.Name,
					})
				return targets
			}

			targets = append(targets,
				ScrapeTarget{
					Addr:      fmt.Sprintf("%s://%s:%d%s", scheme, podIP, cp.ContainerPort, path),
					Namespace: p.Namespace,
					Pod:       p.Name,
					Container: c.Name,
				})
		}
	}

	return targets
}

func parseTargetList(targetList string) (map[string]string, error) {
	// Split the string by ','
	rawTargets := strings.Split(targetList, ",")

	// Initialize the map
	m := make(map[string]string)

	// Iterate over the rawTargets
	for _, rawTarget := range rawTargets {
		// Split each rawTarget by ':'
		targetPair := strings.Split(strings.TrimSpace(rawTarget), ":")
		if len(targetPair) != 2 {
			return nil, fmt.Errorf("Using default scrape rules - target list contains malformed grouping: " + rawTarget)
		}
		// flipping expected order to ensure that port is the key
		m[targetPair[1]] = targetPair[0]
	}

	return m, nil
}

func addTargetFromMap(podIP, scheme, port, namespace, pod, container string, targetMap map[string]string) (ScrapeTarget, bool) {
	if tPath, ok := targetMap[port]; ok {
		target := ScrapeTarget{
			Addr:      fmt.Sprintf("%s://%s:%s%s", scheme, podIP, port, tPath),
			Namespace: namespace,
			Pod:       pod,
			Container: container,
		}
		delete(targetMap, port)
		return target, true
	}
	return ScrapeTarget{}, false
}

func getAnnotationOrDefault(p *v1.Pod, key, def string) string {
	if value, ok := p.Annotations[key]; ok && value != "" {
		return value
	}
	return def
}

func getTargetAnnotationMapOrDefault(p *v1.Pod, key string, defaultVal map[string]string) map[string]string {
	rawVal, exists := p.Annotations[key]
	if !exists || rawVal == "" {
		return defaultVal
	}

	parsedMap, err := parseTargetList(rawVal)
	if err != nil {
		logger.Warnf(err.Error())
		return defaultVal
	}
	return parsedMap
}
