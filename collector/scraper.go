package collector

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/collector/metadata"
	"github.com/Azure/adx-mon/pkg/k8s"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/remote"
	"github.com/Azure/adx-mon/storage"
	"github.com/Azure/adx-mon/transform"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
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

	// ScrapeInterval overrides the global interval. Zero inherits the default.
	ScrapeInterval time.Duration
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
	return t == other
}

// scrapeTargetKey separates pod incarnations and endpoints, including static
// endpoints with identical labels. Scheduling options are not part of identity.
type scrapeTargetKey struct {
	podUID    types.UID
	static    bool
	namespace string
	pod       string
	container string
	addr      string
}

func (t ScrapeTarget) key(uid types.UID, static bool) scrapeTargetKey {
	return scrapeTargetKey{uid, static, t.Namespace, t.Pod, t.Container, t.Addr}
}

func (k scrapeTargetKey) belongsTo(p *v1.Pod) bool {
	return !k.static && k.podUID == p.UID && k.namespace == p.Namespace && k.pod == p.Name
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
	targets map[scrapeTargetKey]ScrapeTarget

	targetsChanged chan struct{}
}

func NewScraper(opts *ScraperOpts) *Scraper {
	return &Scraper{
		podInformer:        opts.PodInformer,
		opts:               *opts,
		seriesCreator:      &seriesCreator{},
		requestTransformer: opts.RequestTransformer(),
		remoteClients:      opts.RemoteClients,
		healthChecker:      opts.HealthChecker,
		targets:            make(map[scrapeTargetKey]ScrapeTarget),
		targetsChanged:     make(chan struct{}, 1),
	}
}

func (s *Scraper) Open(ctx context.Context) error {
	if s.opts.ScrapeInterval <= 0 {
		return errors.New("scrape interval must be greater than zero")
	}
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
		s.targets[target.key("", true)] = target
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

	reconnectTimer := time.NewTicker(5 * time.Minute)
	defer reconnectTimer.Stop()
	t := time.NewTimer(0)
	defer t.Stop()
	timerC := t.C
	schedule := make(scrapeSchedule)
	for {
		select {
		case <-ctx.Done():
			return
		case <-timerC:
		case <-s.targetsChanged:
		case <-reconnectTimer.C:
			for _, remoteClient := range s.remoteClients {
				remoteClient.CloseIdleConnections()
			}
		}

		now := time.Now()
		s.mu.RLock()
		schedule.reconcile(s.targets, s.opts.ScrapeInterval, now)
		s.mu.RUnlock()
		if targets := schedule.due(now); len(targets) > 0 {
			if !s.healthChecker.IsHealthy() {
				logger.Warnf("Collector is unhealthy, skipping scrape")
			} else {
				s.scrapeTargets(ctx, targets)
			}
			schedule.advance(now, time.Now())
		}

		timerC = nil
		t.Stop()
		if next, ok := schedule.next(); ok {
			t.Reset(time.Until(next))
			timerC = t.C
		}
	}
}

func (s *Scraper) scrapeTargets(ctx context.Context, targets []ScrapeTarget) {
	scrapeTime := time.Now().UnixNano() / 1e6
	wr := prompb.WriteRequestPool.Get()
	defer prompb.WriteRequestPool.Put(wr)

	for _, target := range targets {
		if ctx.Err() != nil {
			break
		}
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

	s.flushBatch(ctx, wr)
}

func (s *Scraper) flushBatchIfNecessary(ctx context.Context, wr *prompb.WriteRequest) *prompb.WriteRequest {
	if len(wr.Timeseries) < s.opts.MaxBatchSize {
		return wr
	}

	return s.flushBatch(ctx, wr)
}

func (s *Scraper) flushBatch(ctx context.Context, wr *prompb.WriteRequest) *prompb.WriteRequest {
	if len(wr.Timeseries) == 0 {
		return wr
	}

	original := append([]*prompb.TimeSeries(nil), wr.Timeseries...)
	filtered := s.requestTransformer.TransformWriteRequestWithCommonLabels(wr)
	if len(filtered.Timeseries) > 0 {
		if err := s.sendBatch(ctx, filtered); err != nil {
			logger.Error(err.Error())
		}
	}

	for _, ts := range original {
		prompb.TimeSeriesPool.Put(ts)
	}
	clear(filtered.Timeseries[:cap(filtered.Timeseries)])
	filtered.Timeseries = filtered.Timeseries[:0]
	filtered.CommonLabels = nil
	filtered.LabelFilter = nil
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
			first := true
			for l := range wr.Labels(ts) {
				if !first {
					sb.WriteByte(',')
				}
				sb.Write(l.Name)
				sb.WriteString("=")
				sb.Write(l.Value)
				first = false
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

	_, err := storage.SplitOutcome(remote.WriteRequest(ctx, s.remoteClients, wr))
	return err
}

func (s *Scraper) OnAdd(obj interface{}, isInitialList bool) {
	p, ok := obj.(*v1.Pod)
	if !ok || p == nil {
		return
	}

	targets := s.isScrapeable(p)

	s.mu.Lock()
	defer s.mu.Unlock()

	desired := make(map[scrapeTargetKey]ScrapeTarget, len(targets))
	changed := false
	for _, target := range targets {
		key := target.key(p.UID, false)
		desired[key] = target
		existing, ok := s.targets[key]
		if ok {
			if target.Equals(existing) {
				continue
			}
			logger.Infof("Updating target %s %s", target.path(), target)
		} else {
			logger.Infof("Adding target %s %s", target.path(), target)
		}
		s.targets[key] = target
		changed = true
	}
	for key, target := range s.targets {
		if _, ok := desired[key]; key.belongsTo(p) && !ok {
			logger.Infof("Removing target %s %s", target.path(), target)
			delete(s.targets, key)
			changed = true
		}
	}
	if changed {
		s.notifyTargetsChanged()
	}
}

func (s *Scraper) OnUpdate(oldObj, newObj any) {
	p, ok := newObj.(*v1.Pod)
	if !ok || p == nil {
		return
	}

	if oldPod, ok := oldObj.(*v1.Pod); ok && oldPod != nil && oldPod.UID != p.UID {
		s.OnDelete(oldPod)
	}

	if p.DeletionTimestamp != nil {
		s.OnDelete(p)
		return
	}
	s.OnAdd(p, false)
}

func (s *Scraper) OnDelete(obj interface{}) {
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = tombstone.Obj
	}
	p, ok := obj.(*v1.Pod)
	if !ok || p == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for key, target := range s.targets {
		if key.belongsTo(p) {
			logger.Infof("Removing target %s %s", target.path(), target)
			delete(s.targets, key)
			s.notifyTargetsChanged()
		}
	}
}

// Coalesce informer notifications; the scraper always reconciles the full set.
func (s *Scraper) notifyTargetsChanged() {
	select {
	case s.targetsChanged <- struct{}{}:
	default:
	}
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
		if a[i].path() == a[j].path() {
			return a[i].Addr < a[j].Addr
		}
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
	interval := getScrapeInterval(p)

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
						target.ScrapeInterval = interval
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
						Addr:           fmt.Sprintf("%s://%s:%s%s", scheme, podIP, actualPort, path),
						Namespace:      p.Namespace,
						Pod:            p.Name,
						Container:      c.Name,
						ScrapeInterval: interval,
					})
				return targets
			}

			targets = append(targets,
				ScrapeTarget{
					Addr:           fmt.Sprintf("%s://%s:%d%s", scheme, podIP, cp.ContainerPort, path),
					Namespace:      p.Namespace,
					Pod:            p.Name,
					Container:      c.Name,
					ScrapeInterval: interval,
				})
		}
	}

	return targets
}

func getScrapeInterval(p *v1.Pod) time.Duration {
	raw := getAnnotationOrDefault(p, "adx-mon/scrape-interval", "")
	if raw == "" {
		return 0
	}
	interval, err := time.ParseDuration(raw)
	if err != nil || interval <= 0 {
		logger.Warnf("Invalid adx-mon/scrape-interval %q for pod %s/%s: must be a positive Go duration; using global scrape interval",
			raw, p.Namespace, p.Name)
		return 0
	}
	return interval
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
