package alert

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"k8s.io/utils/clock"
)

const (
	// DefaultMaxQueueSize is the maximum number of alerts queued per output endpoint
	// pending delivery or retry. When a new alert arrives and more than DefaultMaxQueueSize
	// alerts would be queued, the oldest alert at the head of the queue is dropped.
	DefaultMaxQueueSize = 5

	// DefaultInitialBackoff is the initial backoff delay before retrying a failed delivery.
	DefaultInitialBackoff = 500 * time.Millisecond

	// DefaultMaxBackoff is the upper bound on exponential backoff delay between retries.
	DefaultMaxBackoff = 10 * time.Second

	// DefaultMaxRetries is the maximum number of retry attempts per alert before dropping it.
	DefaultMaxRetries = 5

	dropReasonQueueFull        = "queue_full"
	dropReasonRetriesExhausted = "retries_exhausted"
)

// Sender delivers a single alert to a target webhook endpoint.
type Sender interface {
	Create(ctx context.Context, endpoint string, alert Alert) error
}

// FanoutOption configures a FanoutClient.
type FanoutOption func(*fanoutConfig)

type fanoutConfig struct {
	timeout        time.Duration
	sender         Sender
	maxQueueSize   int
	initialBackoff time.Duration
	maxBackoff     time.Duration
	maxRetries     int
	clock          clock.Clock
}

// WithTimeout configures the HTTP client timeout when using the default webhook Sender.
func WithTimeout(timeout time.Duration) FanoutOption {
	return func(cfg *fanoutConfig) {
		if timeout > 0 {
			cfg.timeout = timeout
		}
	}
}

// WithSender overrides the underlying Sender used by each endpoint queue.
func WithSender(sender Sender) FanoutOption {
	return func(cfg *fanoutConfig) {
		cfg.sender = sender
	}
}

// WithMaxQueueSize overrides the per-endpoint bounded queue capacity.
func WithMaxQueueSize(size int) FanoutOption {
	return func(cfg *fanoutConfig) {
		if size > 0 {
			cfg.maxQueueSize = size
		}
	}
}

// WithBackoff overrides the per-endpoint retry backoff parameters.
func WithBackoff(initial, max time.Duration, maxRetries int) FanoutOption {
	return func(cfg *fanoutConfig) {
		if initial > 0 {
			cfg.initialBackoff = initial
		}
		if max > 0 {
			cfg.maxBackoff = max
		}
		if maxRetries >= 0 {
			cfg.maxRetries = maxRetries
		}
	}
}

// WithClock overrides the clock used for retry backoff timers.
func WithClock(c clock.Clock) FanoutOption {
	return func(cfg *fanoutConfig) {
		if c != nil {
			cfg.clock = c
		}
	}
}

// NormalizeEndpoint normalizes an alerter notification base address to its /alerts endpoint.
func NormalizeEndpoint(addr string) string {
	addr = strings.TrimSpace(addr)
	addr = strings.TrimRight(addr, "/")
	if addr == "" {
		return ""
	}
	if strings.HasSuffix(addr, "/alerts") {
		return addr
	}
	return addr + "/alerts"
}

// FanoutClient fans out each Alert to N webhook endpoints. Each endpoint has its own
// bounded FIFO queue, single ordered delivery worker, exponential backoff retry, and
// drop-oldest eviction when more than maxQueueSize messages are pending.
type FanoutClient struct {
	queues []*endpointQueue

	mu      sync.Mutex
	opened  bool
	closeFn context.CancelFunc
	wg      sync.WaitGroup
}

// NewFanoutClient creates a FanoutClient for the provided webhook addresses.
func NewFanoutClient(endpoints []string, opts ...FanoutOption) (*FanoutClient, error) {
	cfg := &fanoutConfig{
		timeout:        time.Minute,
		maxQueueSize:   DefaultMaxQueueSize,
		initialBackoff: DefaultInitialBackoff,
		maxBackoff:     DefaultMaxBackoff,
		maxRetries:     DefaultMaxRetries,
		clock:          clock.RealClock{},
	}
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.sender == nil {
		httpCli, err := NewClient(cfg.timeout)
		if err != nil {
			return nil, err
		}
		cfg.sender = httpCli
	}

	seen := make(map[string]struct{}, len(endpoints))
	var queues []*endpointQueue
	for _, raw := range endpoints {
		ep := NormalizeEndpoint(raw)
		if ep == "" {
			continue
		}
		if _, exists := seen[ep]; exists {
			continue
		}
		seen[ep] = struct{}{}
		queues = append(queues, newEndpointQueue(ep, cfg))
	}

	if len(queues) == 0 {
		return nil, fmt.Errorf("at least one notification endpoint is required")
	}

	return &FanoutClient{
		queues: queues,
	}, nil
}

// Endpoints returns the normalized webhook URLs configured on this client.
func (f *FanoutClient) Endpoints() []string {
	out := make([]string, len(f.queues))
	for i, q := range f.queues {
		out[i] = q.endpoint
	}
	return out
}

// Open starts the per-endpoint delivery workers.
func (f *FanoutClient) Open(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.opened {
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	f.closeFn = cancel
	f.opened = true

	for _, q := range f.queues {
		f.wg.Add(1)
		go func(eq *endpointQueue) {
			defer f.wg.Done()
			eq.run(runCtx)
		}(q)
	}
	return nil
}

// Close stops all per-endpoint delivery workers and waits for them to exit.
func (f *FanoutClient) Close() error {
	f.mu.Lock()
	cancel := f.closeFn
	f.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	f.wg.Wait()
	return nil
}

// Create enqueues a copy of alert into every configured endpoint's bounded FIFO queue.
// The endpoint parameter is accepted to satisfy the engine.AlertCli interface; FanoutClient
// routes to all endpoints configured at construction time.
func (f *FanoutClient) Create(ctx context.Context, _ string, a Alert) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, q := range f.queues {
		q.enqueue(a)
	}
	return nil
}

type queuedAlert struct {
	id      uint64
	alert   Alert
	evicted chan struct{}
}

type endpointQueue struct {
	endpoint       string
	sender         Sender
	maxQueueSize   int
	initialBackoff time.Duration
	maxBackoff     time.Duration
	maxRetries     int
	clock          clock.Clock

	mu       sync.Mutex
	queue    []queuedAlert
	nextID   uint64
	notifyCh chan struct{}
}

func newEndpointQueue(endpoint string, cfg *fanoutConfig) *endpointQueue {
	return &endpointQueue{
		endpoint:       endpoint,
		sender:         cfg.sender,
		maxQueueSize:   cfg.maxQueueSize,
		initialBackoff: cfg.initialBackoff,
		maxBackoff:     cfg.maxBackoff,
		maxRetries:     cfg.maxRetries,
		clock:          cfg.clock,
		queue:          make([]queuedAlert, 0, cfg.maxQueueSize),
		notifyCh:       make(chan struct{}, 1),
	}
}

func (q *endpointQueue) enqueue(a Alert) {
	q.mu.Lock()
	q.nextID++
	item := queuedAlert{
		id:      q.nextID,
		alert:   cloneAlert(a),
		evicted: make(chan struct{}),
	}

	if len(q.queue) >= q.maxQueueSize {
		dropped := q.queue[0]
		copy(q.queue, q.queue[1:])
		q.queue[len(q.queue)-1] = item
		close(dropped.evicted)
		q.mu.Unlock()

		metrics.NotificationsDroppedTotal.WithLabelValues(q.endpoint, dropReasonQueueFull).Inc()
		logger.Warnf("Notification queue full (%d) for %s; dropping oldest alert %q (source=%s) and trying next",
			q.maxQueueSize, q.endpoint, dropped.alert.Title, dropped.alert.Source)
		q.signalNotify()
		return
	}

	q.queue = append(q.queue, item)
	q.mu.Unlock()
	q.signalNotify()
}

func (q *endpointQueue) signalNotify() {
	select {
	case q.notifyCh <- struct{}{}:
	default:
	}
}

func (q *endpointQueue) run(ctx context.Context) {
	for {
		curr, ok := q.peek()
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-q.notifyCh:
				continue
			}
		}

		q.deliverHead(ctx, curr)
		if ctx.Err() != nil {
			return
		}
	}
}

func (q *endpointQueue) deliverHead(ctx context.Context, curr queuedAlert) {
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return
		}
		select {
		case <-curr.evicted:
			return
		default:
		}

		reqCtx, reqCancel := context.WithCancel(ctx)
		stopWatch := make(chan struct{})
		go func() {
			select {
			case <-curr.evicted:
				reqCancel()
			case <-stopWatch:
			}
		}()

		err := q.sender.Create(reqCtx, q.endpoint, curr.alert)
		close(stopWatch)
		reqCancel()

		if err == nil {
			q.popIfHead(curr.id)
			return
		}

		if ctx.Err() != nil {
			return
		}

		select {
		case <-curr.evicted:
			return
		default:
		}

		if attempt >= q.maxRetries {
			metrics.NotificationsDroppedTotal.WithLabelValues(q.endpoint, dropReasonRetriesExhausted).Inc()
			logger.Errorf("Failed to deliver alert %q (source=%s) to %s after %d retries: %v",
				curr.alert.Title, curr.alert.Source, q.endpoint, attempt, err)
			q.popIfHead(curr.id)
			return
		}

		delay := q.backoffDelay(attempt)
		logger.Warnf("Failed to deliver alert %q (source=%s) to %s (attempt %d/%d), retrying in %s: %v",
			curr.alert.Title, curr.alert.Source, q.endpoint, attempt+1, q.maxRetries+1, delay, err)

		timer := q.clock.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-curr.evicted:
			timer.Stop()
			return
		case <-timer.C():
		}
	}
}

func (q *endpointQueue) peek() (queuedAlert, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.queue) == 0 {
		return queuedAlert{}, false
	}
	return q.queue[0], true
}

func (q *endpointQueue) popIfHead(id uint64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.queue) > 0 && q.queue[0].id == id {
		copy(q.queue, q.queue[1:])
		q.queue[len(q.queue)-1] = queuedAlert{}
		q.queue = q.queue[:len(q.queue)-1]
	}
}

func (q *endpointQueue) backoffDelay(attempt int) time.Duration {
	delay := q.initialBackoff
	for range attempt {
		delay *= 2
		if delay >= q.maxBackoff {
			return q.maxBackoff
		}
	}
	if delay > q.maxBackoff {
		return q.maxBackoff
	}
	return delay
}

func cloneAlert(a Alert) Alert {
	cloned := a
	if a.CustomFields != nil {
		cloned.CustomFields = make(map[string]string, len(a.CustomFields))
		for k, v := range a.CustomFields {
			cloned.CustomFields[k] = v
		}
	}
	return cloned
}
