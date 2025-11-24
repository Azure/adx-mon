package ingestor

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	"k8s.io/utils/clock"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// ErrNoPodsScheduled indicates that no ingestor pods were scheduled on any node.
	ErrNoPodsScheduled = errors.New("no ingestor pods scheduled")

	// ErrMissingNodeMetrics indicates that the metrics API did not return usage data for a required node.
	ErrMissingNodeMetrics = errors.New("missing node metrics")
)

// NodeMetricsClient exposes the subset of the metrics client used by the collector.
type NodeMetricsClient interface {
	List(ctx context.Context, opts metav1.ListOptions) (*metricsv1beta1.NodeMetricsList, error)
}

type sample struct {
	ts    time.Time
	value float64
}

// Collector computes CPU utilization for nodes hosting ingestor pods and retains historical samples.
type Collector struct {
	kubeClient    ctrlclient.Client
	metricsClient NodeMetricsClient
	namespace     string
	statefulSet   string
	clock         clock.Clock
	labelSelector labels.Selector

	mu      sync.Mutex
	samples []sample
}

// CollectorOption customizes Collector behavior.
type CollectorOption func(*Collector)

// WithLabelSelector overrides the label selector used to identify ingestor pods.
func WithLabelSelector(selector labels.Selector) CollectorOption {
	return func(c *Collector) {
		if selector != nil {
			c.labelSelector = selector
		}
	}
}

// NewCollector constructs a Collector for the provided namespace and statefulset name.
func NewCollector(kubeClient ctrlclient.Client, metricsClient NodeMetricsClient, namespace, statefulSet string, clk clock.Clock, opts ...CollectorOption) *Collector {
	if clk == nil {
		clk = clock.RealClock{}
	}
	collector := &Collector{
		kubeClient:    kubeClient,
		metricsClient: metricsClient,
		namespace:     namespace,
		statefulSet:   statefulSet,
		clock:         clk,
		labelSelector: DefaultLabelSelector(),
	}
	for _, opt := range opts {
		opt(collector)
	}
	return collector
}

// AverageCPU returns the average CPU utilization percentage across nodes hosting ingestor pods.
// The result is averaged across historical samples within the provided window. The method always captures
// a fresh sample prior to computing the rolling average.
func (c *Collector) AverageCPU(ctx context.Context, window time.Duration) (float64, error) {
	if window <= 0 {
		return 0, errors.New("window must be positive")
	}

	if err := c.captureSample(ctx); err != nil {
		return 0, err
	}

	cutoff := c.clock.Now().Add(-window)

	c.mu.Lock()
	defer c.mu.Unlock()

	// drop stale samples while keeping the slice capacity for reuse
	idx := 0
	for _, s := range c.samples {
		if s.ts.After(cutoff) {
			c.samples[idx] = s
			idx++
		}
	}
	c.samples = c.samples[:idx]

	if len(c.samples) == 0 {
		return 0, ErrNoPodsScheduled
	}

	var sum float64
	for _, s := range c.samples {
		sum += s.value
	}

	return sum / float64(len(c.samples)), nil
}

// captureSample measures instantaneous CPU utilization and stores it as a rolling sample.
func (c *Collector) captureSample(ctx context.Context) error {
	pods := &corev1.PodList{}
	selector := c.labelSelector
	if selector == nil {
		selector = DefaultLabelSelector()
	}
	if err := c.kubeClient.List(ctx, pods,
		ctrlclient.InNamespace(c.namespace),
		ctrlclient.MatchingLabelsSelector{Selector: selector},
	); err != nil {
		return err
	}

	nodes := sets.New[string]()
	for _, pod := range pods.Items {
		if !strings.HasPrefix(pod.Name, c.statefulSet+"-") {
			continue
		}
		if pod.Spec.NodeName == "" {
			continue
		}
		nodes.Insert(pod.Spec.NodeName)
	}

	if nodes.Len() == 0 {
		return ErrNoPodsScheduled
	}

	// Fetch metrics for all nodes and build lookup map.
	nodeMetricsList, err := c.metricsClient.List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	metricsByName := make(map[string]metricsv1beta1.NodeMetrics, len(nodeMetricsList.Items))
	for _, nm := range nodeMetricsList.Items {
		metricsByName[nm.Name] = nm
	}

	var ( // gather nodes to fetch capacities deterministically
		nodeNames    = nodes.UnsortedList()
		utilizations []float64
	)
	sort.Strings(nodeNames)

	for _, nodeName := range nodeNames {
		metrics, ok := metricsByName[nodeName]
		if !ok {
			return ErrMissingNodeMetrics
		}

		node := &corev1.Node{}
		if err := c.kubeClient.Get(ctx, types.NamespacedName{Name: nodeName}, node); err != nil {
			return err
		}

		usageMilli := metrics.Usage.Cpu().MilliValue()
		capacityMilli := node.Status.Capacity.Cpu().MilliValue()
		if capacityMilli == 0 {
			continue
		}

		utilization := (float64(usageMilli) / float64(capacityMilli)) * 100
		utilizations = append(utilizations, utilization)
	}

	if len(utilizations) == 0 {
		return ErrMissingNodeMetrics
	}

	var sum float64
	for _, u := range utilizations {
		sum += u
	}

	avg := sum / float64(len(utilizations))

	c.mu.Lock()
	c.samples = append(c.samples, sample{ts: c.clock.Now(), value: avg})
	c.mu.Unlock()

	return nil
}
