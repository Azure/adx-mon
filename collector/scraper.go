package collector

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/collector/metadata"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/k8s"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/remote"
	"github.com/Azure/adx-mon/transform"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

type ScraperOpts struct {
	NodeName string

	DefaultDropMetrics bool

	// AddLabels is a map of key/value pairs that will be added to all metrics.
	AddLabels map[string]string

	// DropLabels is a map of metric names regexes to label name regexes.  When both match, the label will be dropped.
	DropLabels map[*regexp.Regexp]*regexp.Regexp

	// DropMetrics is a slice of regexes that drops metrics when the metric name matches.  The metric name format
	// should match the Prometheus naming style before the metric is translated to a Kusto table name.
	DropMetrics []*regexp.Regexp

	KeepMetricsWithLabelValue map[*regexp.Regexp]*regexp.Regexp

	KeepMetrics []*regexp.Regexp

	// Database is the name of the database to write metrics to.
	Database string

	// ScrapeInterval is the interval at which to scrape metrics from the targets.
	ScrapeInterval time.Duration

	// ScrapeTimeout is the timeout for scraping metrics from a target.
	ScrapeTimeout time.Duration

	// DisableMetricsForwarding disables the forwarding of metrics to the remote write endpoint.
	DisableMetricsForwarding bool

	// DisableDiscovery disables the discovery of scrape targets.
	DisableDiscovery bool

	PodInformer k8s.PodInformerInterface

	// Targets is a list of static scrape targets.
	Targets []ScrapeTarget

	// MaxBatchSize is the maximum number of samples to send in a single batch.
	MaxBatchSize int

	RemoteClients []remote.RemoteWriteClient

	HealthChecker interface{ IsHealthy() bool }

	DynamicLabeler metadata.MetricLabeler
}

func (s *ScraperOpts) RequestTransformer() *transform.RequestTransformer {
	return &transform.RequestTransformer{
		DefaultDropMetrics:        s.DefaultDropMetrics,
		AddLabels:                 s.AddLabels,
		DropLabels:                s.DropLabels,
		DropMetrics:               s.DropMetrics,
		KeepMetrics:               s.KeepMetrics,
		KeepMetricsWithLabelValue: s.KeepMetricsWithLabelValue,
		DynamicLabeler:            s.DynamicLabeler,
	}
}

type ScrapeTarget struct {
	Static    bool
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

func (t ScrapeTarget) Equals(other ScrapeTarget) bool {
	return t.Addr == other.Addr && t.Namespace == other.Namespace && t.Pod == other.Pod && t.Container == other.Container && t.Static == other.Static
}

type Scraper struct {
	opts                 ScraperOpts
	podInformer          k8s.PodInformerInterface
	informerRegistration cache.ResourceEventHandlerRegistration

	requestTransformer *transform.RequestTransformer
	remoteClients      []remote.RemoteWriteClient
	scrapeClient       *MetricsClient
	seriesCreator      *seriesCreator
	healthChecker      interface{ IsHealthy() bool }

	wg     sync.WaitGroup
	cancel context.CancelFunc

	mu      sync.RWMutex
	targets map[string]ScrapeTarget
}

func NewScraper(opts *ScraperOpts) *Scraper {
	return &Scraper{
		podInformer:        opts.PodInformer,
		opts:               *opts,
		seriesCreator:      &seriesCreator{},
		requestTransformer: opts.RequestTransformer(),
		remoteClients:      opts.RemoteClients,
		healthChecker:      opts.HealthChecker,
		targets:            make(map[string]ScrapeTarget),
	}
}

func (s *Scraper) Open(ctx context.Context) error {
	logger.Infof("Starting prometheus scraper for node %s", s.opts.NodeName)
	ctx, cancelFn := context.WithCancel(ctx)
	s.cancel = cancelFn

	var err error
	s.scrapeClient, err = NewMetricsClient(ClientOpts{
		ScrapeTimeOut: s.opts.ScrapeTimeout,
		NodeName:      s.opts.NodeName,
	})
	if err != nil {
		return fmt.Errorf("failed to create metrics client: %w", err)
	}

	// Add static targets
	for _, target := range s.opts.Targets {
		logger.Infof("Adding static target %s", target)
		s.targets[target.path()] = target
	}

	if !s.opts.DisableDiscovery {
		s.informerRegistration, err = s.podInformer.Add(ctx, s)
		if err != nil {
			return fmt.Errorf("failed to add pod informer: %w", err)
		}
	}

	// Discover the initial targets running on the node
	s.wg.Add(1)
	go s.scrape(ctx)

	return nil
}

func (s *Scraper) Close() error {
	s.scrapeClient.Close()
	s.cancel()
	if !s.opts.DisableDiscovery {
		s.podInformer.Remove(s.informerRegistration)
	}
	s.informerRegistration = nil
	s.wg.Wait()

	return nil
}

func (s *Scraper) scrape(ctx context.Context) {
	defer s.wg.Done()

	// Sleep for a random amount of time to avoid thundering herd
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Duration(rand.Intn(int(s.opts.ScrapeInterval.Seconds()))) * time.Second):
		// continue
	}

	reconnectTimer := time.NewTicker(5 * time.Minute)
	defer reconnectTimer.Stop()
	t := time.NewTicker(s.opts.ScrapeInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !s.healthChecker.IsHealthy() {
				logger.Warnf("Collector is unhealthy, skipping scrape")
				continue
			}
			s.scrapeTargets(ctx)
		case <-reconnectTimer.C:
			for _, remoteClient := range s.remoteClients {
				remoteClient.CloseIdleConnections()
			}
		}
	}
}

func (s *Scraper) scrapeTargets(ctx context.Context) {
	targets := s.Targets()

	scrapeTime := time.Now().UnixNano() / 1e6
	wr := prompb.WriteRequestPool.Get()
	defer prompb.WriteRequestPool.Put(wr)

	for _, target := range targets {
		logger.Infof("Scraping %s", target.String())
		iter, err := s.scrapeClient.FetchMetricsIterator(target.Addr)
		if err != nil {
			logger.Warnf("Failed to create scrape iterator %s/%s/%s at %s: %s",
				target.Namespace, target.Pod, target.Container, target.Addr, err.Error())
			continue
		}
		for iter.Next() {
			pt := prompb.TimeSeriesPool.Get()
			ts, err := iter.TimeSeriesInto(pt)
			if err != nil {
				logger.Warnf("Failed to parse series %s/%s/%s at %s: %s",
					target.Namespace, target.Pod, target.Container, target.Addr, err.Error())
				continue
			}

			name := prompb.MetricName(ts)
			if s.requestTransformer.ShouldDropMetric(ts, name) {
				prompb.TimeSeriesPool.Put(ts)
				if metrics.DebugMetricsEnabled {
					metrics.MetricsDroppedTotal.WithLabelValues(string(name)).Add(1)
				}
				continue
			}

			for i, s := range ts.Samples {
				if s.Timestamp == 0 {
					s.Timestamp = scrapeTime
				}
				ts.Samples[i] = s
			}

			if target.Namespace != "" {
				ts.AppendLabelString("adxmon_namespace", target.Namespace)
			}

			if target.Pod != "" {
				ts.AppendLabelString("adxmon_pod", target.Pod)
			}

			if target.Container != "" {
				ts.AppendLabelString("adxmon_container", target.Container)
			}

			prompb.Sort(ts.Labels)

			ts = s.requestTransformer.TransformTimeSeries(ts)
			wr.Timeseries = append(wr.Timeseries, ts)
			wr = s.flushBatchIfNecessary(ctx, wr)
		}
		if err := iter.Err(); err != nil {
			logger.Warnf("Failed to scrape %s/%s/%s at %s: %s", target.Namespace, target.Pod, target.Container, target.Addr, err.Error())
		}

		if err := iter.Close(); err != nil {
			logger.Errorf("Failed to close iterator for %s/%s/%s: %s", target.Namespace, target.Pod, target.Container, err.Error())
		}

		wr = s.flushBatchIfNecessary(ctx, wr)
	}

	if err := s.sendBatch(ctx, wr); err != nil {
		logger.Error(err.Error())
	}
	wr.Timeseries = wr.Timeseries[:0]
}

func (s *Scraper) flushBatchIfNecessary(ctx context.Context, wr *prompb.WriteRequest) *prompb.WriteRequest {
	filtered := wr
	if len(filtered.Timeseries) >= s.opts.MaxBatchSize {
		filtered = s.requestTransformer.TransformWriteRequest(wr)
	}

	if len(filtered.Timeseries) >= s.opts.MaxBatchSize {
		if err := s.sendBatch(ctx, filtered); err != nil {
			logger.Error(err.Error())
		}
		for i := range filtered.Timeseries {
			ts := filtered.Timeseries[i]
			prompb.TimeSeriesPool.Put(ts)
		}
		filtered.Timeseries = filtered.Timeseries[:0]
	}
	return filtered
}

func (s *Scraper) sendBatch(ctx context.Context, wr *prompb.WriteRequest) error {
	if len(wr.Timeseries) == 0 {
		return nil
	}

	if len(s.remoteClients) == 0 || logger.IsDebug() {
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

	start := time.Now()
	defer func() {
		logger.Infof("Sending %d timeseries to %d endpoints duration=%s", len(wr.Timeseries), len(s.remoteClients), time.Since(start))
	}()

	return remote.WriteRequest(ctx, s.remoteClients, wr)
}

func (s *Scraper) OnAdd(obj interface{}, isInitialList bool) {
	p, ok := obj.(*v1.Pod)
	if !ok || p == nil {
		return
	}

	targets := s.isScrapeable(p)

	// Not a scrape-able pod
	if len(targets) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, target := range targets {
		existing, ok := s.targets[string(p.UID)]
		if ok {
			if target.Equals(existing) {
				return
			}
			logger.Infof("Updating target %s %s", target.path(), target)
		} else {
			logger.Infof("Adding target %s %s", target.path(), target)
		}
		s.targets[string(p.UID)] = target
	}
}

func (s *Scraper) OnUpdate(oldObj, newObj interface{}) {
	p, ok := newObj.(*v1.Pod)
	if !ok || p == nil {
		return
	}

	if p.DeletionTimestamp != nil {
		s.OnDelete(p)
		return
	}
	s.OnAdd(p, false)
}

func (s *Scraper) OnDelete(obj interface{}) {
	p, ok := obj.(*v1.Pod)
	if !ok || p == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	target, ok := s.targets[string(p.UID)]
	if !ok {
		return
	}
	logger.Infof("Removing target %s %s", target.path(), target)
	delete(s.targets, string(p.UID))
}

// isScrapeable returns the scrape target endpoints and true if the pod is currently a target, false otherwise
func (s *Scraper) isScrapeable(p *v1.Pod) []ScrapeTarget {
	return makeTargets(p)
}

func (s *Scraper) Targets() []ScrapeTarget {
	s.mu.RLock()
	defer s.mu.RUnlock()

	a := make([]ScrapeTarget, 0, len(s.targets))
	for _, v := range s.targets {
		a = append(a, v)
	}
	sort.Slice(a, func(i, j int) bool {
		return a[i].path() < a[j].path()
	})
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
				// Add named port to the check list if it exists
				if cp.Name != "" {
					checkPorts = append(checkPorts, cp.Name)
				}
				for _, checkPort := range checkPorts {
					// if the current port, liveness port, readiness port, or named port exist in the targetMap, add that to scrape targets
					if target, added := addTargetFromMap(podIP, scheme, checkPort, p.Namespace, p.Name, c.Name, targetMap, &cp); added {
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
				portMatch := false
				actualPort := ""

				// Check if it matches numeric port
				if port == strconv.Itoa(int(cp.ContainerPort)) || port == readinessPort || port == livenessPort {
					portMatch = true
					actualPort = port
				}
				// Check if it matches named port
				if cp.Name != "" && port == cp.Name {
					portMatch = true
					actualPort = strconv.Itoa(int(cp.ContainerPort))
				}

				if !portMatch {
					continue
				}

				targets = append(targets,
					ScrapeTarget{
						Addr:      fmt.Sprintf("%s://%s:%s%s", scheme, podIP, actualPort, path),
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
			return nil, errors.New("using default scrape rules - target list contains malformed grouping: " + rawTarget)
		}
		// flipping expected order to ensure that port is the key
		m[targetPair[1]] = targetPair[0]
	}

	return m, nil
}

func addTargetFromMap(podIP, scheme, port, namespace, pod, container string, targetMap map[string]string, cp *v1.ContainerPort) (ScrapeTarget, bool) {
	if tPath, ok := targetMap[port]; ok {
		actualPort := port
		// If the port matches a named port, use the actual numeric port
		if cp != nil && cp.Name == port {
			actualPort = strconv.Itoa(int(cp.ContainerPort))
		}
		target := ScrapeTarget{
			Addr:      fmt.Sprintf("%s://%s:%s%s", scheme, podIP, actualPort, tPath),
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
		logger.Warn(err.Error())
		return defaultVal
	}
	return parsedMap
}
