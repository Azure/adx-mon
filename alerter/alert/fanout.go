package alert

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math/rand/v2"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"k8s.io/utils/clock"
)

const (
	// DefaultWorkersPerEndpoint is the default number of concurrent HTTP delivery workers per endpoint.
	DefaultWorkersPerEndpoint = 10

	// DefaultRequestTimeout is the default per-POST HTTP timeout.
	DefaultRequestTimeout = 60 * time.Second

	// DefaultDrainTimeout is the maximum time Close waits for queued and in-flight deliveries to drain.
	DefaultDrainTimeout = 50 * time.Second

	// DefaultMaxQueueItems is the maximum number of queued notifications waiting per endpoint.
	DefaultMaxQueueItems = 1024

	// DefaultMaxQueueBytes is the maximum total serialized payload bytes queued per endpoint (16 MiB).
	DefaultMaxQueueBytes = 16 << 20

	// DefaultMaxItemAge is the maximum age (TTL) of a notification from enqueue time before it is dropped.
	DefaultMaxItemAge = 5 * time.Minute

	// DefaultInitialBackoff is the initial backoff delay before retrying a transient delivery failure.
	DefaultInitialBackoff = 500 * time.Millisecond

	// DefaultMaxBackoff is the upper bound on exponential backoff delay between retries.
	DefaultMaxBackoff = 10 * time.Second

	// DefaultMaxRetries is the maximum number of retry attempts per alert after the initial attempt.
	DefaultMaxRetries = 3

	maxSuppressedSummaryDetails = 15

	OutcomeSuccess        = "success"
	OutcomeRetry          = "retry"
	OutcomeThrottled      = "throttled"
	OutcomePermanentError = "permanent_error"

	DropReasonOverflow         = "overflow"
	DropReasonTTLExpired       = "ttl_expired"
	DropReasonRetriesExhausted = "retries_exhausted"
	DropReasonPermanentError   = "permanent_error"
	DropReasonThrottled        = "throttled"
	DropReasonShutdown         = "shutdown"
)

// ErrClientClosed is returned when attempting to enqueue an alert after FanoutClient shutdown has begun.
var ErrClientClosed = errors.New("alert fanout client is closed")

// Endpoint holds a validated, normalized webhook target URL and its redacted identifier.
type Endpoint struct {
	// URL is the full normalized "<base>/alerts[?query]" target used strictly for HTTP delivery.
	URL string
	// ID is the redacted identifier "<scheme>://<host>[:<port>]/#<sha256[:8]>" safe for logs, metrics, and errors.
	ID string
}

// ParseBaseEndpoint validates a webhook base address and structurally joins "/alerts"
// while preserving query parameters and never stripping an existing "/alerts" path suffix.
func ParseBaseEndpoint(raw string) (Endpoint, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Endpoint{}, errors.New("notification endpoint must not be empty")
	}

	u, err := parseAndValidateHTTPURL(trimmed)
	if err != nil {
		return Endpoint{}, err
	}

	scheme, hostPort := canonicalOrigin(u)
	baseCopy := *u
	baseCopy.Scheme = scheme
	baseCopy.Host = hostPort
	baseCopy.Fragment = ""
	baseCopy.RawFragment = ""

	alertsURL := baseCopy.JoinPath("alerts")
	if !strings.HasPrefix(alertsURL.Path, "/") {
		alertsURL.Path = "/" + alertsURL.Path
	}

	normalized := alertsURL.String()
	return Endpoint{
		URL: normalized,
		ID:  formatEndpointID(scheme, hostPort, normalized),
	}, nil
}

// ResolveBaseEndpoints validates and deduplicates webhook base addresses in order.
func ResolveBaseEndpoints(rawAddrs []string) ([]Endpoint, error) {
	seen := make(map[string]struct{}, len(rawAddrs))
	out := make([]Endpoint, 0, len(rawAddrs))
	for _, raw := range rawAddrs {
		ep, err := ParseBaseEndpoint(raw)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[ep.URL]; ok {
			continue
		}
		seen[ep.URL] = struct{}{}
		out = append(out, ep)
	}
	return out, nil
}

// RedactURL returns "<scheme>://<host>[:<port>]/#<sha256[:8]>" for a target HTTP(S) URL,
// stripping credentials, path, query, and fragment. Invalid URLs return "<redacted-endpoint>".
func RedactURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "<empty-endpoint>"
	}
	u, err := parseAndValidateHTTPURL(trimmed)
	if err != nil {
		return "<redacted-endpoint>"
	}
	scheme, hostPort := canonicalOrigin(u)
	clean := *u
	clean.Scheme = scheme
	clean.Host = hostPort
	clean.Fragment = ""
	clean.RawFragment = ""
	return formatEndpointID(scheme, hostPort, clean.String())
}

func parseAndValidateHTTPURL(trimmed string) (*url.URL, error) {
	u, err := url.Parse(trimmed)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			return nil, fmt.Errorf("invalid notification endpoint URL: %w", ue.Err)
		}
		return nil, errors.New("invalid notification endpoint URL")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("invalid notification endpoint scheme %q: must be http or https", u.Scheme)
	}
	if u.Opaque != "" || u.Host == "" || u.Hostname() == "" {
		return nil, errors.New("invalid notification endpoint: host must not be empty")
	}
	return u, nil
}

func canonicalOrigin(u *url.URL) (scheme string, hostPort string) {
	scheme = strings.ToLower(u.Scheme)
	hostname := strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" {
		hostPort = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		hostPort = "[" + hostname + "]"
	} else {
		hostPort = hostname
	}
	return scheme, hostPort
}

func formatEndpointID(scheme, hostPort, normalizedURL string) string {
	sum := sha256.Sum256([]byte(normalizedURL))
	return fmt.Sprintf("%s://%s/#%x", scheme, hostPort, sum[:4])
}

// Sender delivers a single alert to a target webhook endpoint.
type Sender interface {
	Create(ctx context.Context, endpoint string, alert Alert) error
}

// SuppressedAlert records minimal metadata for an alert suppressed after HTTP 429 throttling.
type SuppressedAlert struct {
	Title    string
	Severity int
}

// EvalMeta describes an AlertRule query evaluation for per-endpoint 429 suppression and summary emission.
type EvalMeta struct {
	Namespace         string
	Name              string
	Destination       string
	Region            string
	BuildSummaryAlert func(suppressed []SuppressedAlert, totalSuppressed int, incomplete bool) Alert
}

// FanoutOption configures a FanoutClient.
type FanoutOption func(*fanoutConfig)

type fanoutConfig struct {
	requestTimeout     time.Duration
	drainTimeout       time.Duration
	workersPerEndpoint int
	sender             Sender
	maxQueueItems      int
	maxQueueBytes      int
	maxItemAge         time.Duration
	initialBackoff     time.Duration
	maxBackoff         time.Duration
	maxRetries         int
	clock              clock.Clock
	jitterFn           func(base time.Duration) time.Duration
}

// WithTimeout configures the per-POST HTTP timeout.
func WithTimeout(timeout time.Duration) FanoutOption {
	return func(cfg *fanoutConfig) {
		if timeout > 0 {
			cfg.requestTimeout = timeout
		}
	}
}

// WithDrainTimeout configures the graceful shutdown drain deadline on Close.
func WithDrainTimeout(timeout time.Duration) FanoutOption {
	return func(cfg *fanoutConfig) {
		if timeout > 0 {
			cfg.drainTimeout = timeout
		}
	}
}

// WithConcurrency configures the per-endpoint delivery worker pool size (clamped to >= 1).
func WithConcurrency(workers int) FanoutOption {
	return func(cfg *fanoutConfig) {
		cfg.workersPerEndpoint = max(1, workers)
	}
}

// WithSender overrides the underlying HTTP Sender used by each endpoint worker pool.
func WithSender(sender Sender) FanoutOption {
	return func(cfg *fanoutConfig) {
		cfg.sender = sender
	}
}

// WithMaxQueueItems overrides the per-endpoint maximum queued item count.
func WithMaxQueueItems(items int) FanoutOption {
	return func(cfg *fanoutConfig) {
		if items > 0 {
			cfg.maxQueueItems = items
		}
	}
}

// WithMaxQueueBytes overrides the per-endpoint maximum queued payload byte budget.
func WithMaxQueueBytes(maxBytes int) FanoutOption {
	return func(cfg *fanoutConfig) {
		if maxBytes > 0 {
			cfg.maxQueueBytes = maxBytes
		}
	}
}

// WithMaxItemAge overrides the per-item queue/retry TTL measured from enqueue time.
func WithMaxItemAge(ttl time.Duration) FanoutOption {
	return func(cfg *fanoutConfig) {
		if ttl > 0 {
			cfg.maxItemAge = ttl
		}
	}
}

// WithBackoff overrides the per-endpoint retry backoff parameters.
func WithBackoff(initial, maxDelay time.Duration, maxRetries int) FanoutOption {
	return func(cfg *fanoutConfig) {
		if initial > 0 {
			cfg.initialBackoff = initial
		}
		if maxDelay > 0 {
			cfg.maxBackoff = maxDelay
		}
		if maxRetries >= 0 {
			cfg.maxRetries = maxRetries
		}
	}
}

// WithClock overrides the clock used for TTLs, per-POST timeouts, retry timers, and drain deadlines.
func WithClock(c clock.Clock) FanoutOption {
	return func(cfg *fanoutConfig) {
		if c != nil {
			cfg.clock = c
		}
	}
}

// WithJitter overrides the backoff jitter function used on transient retries.
func WithJitter(fn func(base time.Duration) time.Duration) FanoutOption {
	return func(cfg *fanoutConfig) {
		if fn != nil {
			cfg.jitterFn = fn
		}
	}
}

func defaultJitter(base time.Duration) time.Duration {
	if base <= 1 {
		return base
	}
	half := base / 2
	span := int64(base - half)
	return half + time.Duration(rand.Int64N(span+1))
}

type ruleKey struct {
	namespace string
	name      string
}

type ruleHealthTracker struct {
	mu        sync.Mutex
	unhealthy map[ruleKey]map[string]struct{}
}

func newRuleHealthTracker() *ruleHealthTracker {
	return &ruleHealthTracker{
		unhealthy: make(map[ruleKey]map[string]struct{}),
	}
}

func (h *ruleHealthTracker) recordSuccess(rk ruleKey, endpointID string) {
	if rk.namespace == "" || rk.name == "" {
		return
	}
	h.mu.Lock()
	if eps, ok := h.unhealthy[rk]; ok {
		delete(eps, endpointID)
		if len(eps) > 0 {
			h.mu.Unlock()
			return
		}
		delete(h.unhealthy, rk)
	}
	metrics.NotificationUnhealthy.WithLabelValues(rk.namespace, rk.name).Set(0)
	h.mu.Unlock()
}

func (h *ruleHealthTracker) recordFailure(rk ruleKey, endpointID string) {
	if rk.namespace == "" || rk.name == "" {
		return
	}
	h.mu.Lock()
	eps, ok := h.unhealthy[rk]
	if !ok {
		eps = make(map[string]struct{})
		h.unhealthy[rk] = eps
	}
	eps[endpointID] = struct{}{}
	metrics.NotificationUnhealthy.WithLabelValues(rk.namespace, rk.name).Set(1)
	h.mu.Unlock()
}

// FanoutClient fans out each Alert to N webhook endpoints with isolated per-endpoint
// worker pools, bounded queues, exponential backoff retries, and per-(endpoint, evalID)
// HTTP 429 suppression and summary barriers.
type FanoutClient struct {
	queues       []*endpointQueue
	drainTimeout time.Duration
	clock        clock.Clock

	nextEvalID atomic.Uint64

	mu           sync.Mutex
	opened       bool
	closeStarted bool
	closed       bool
	workerCtx    context.Context
	workerCancel context.CancelFunc
	wg           sync.WaitGroup
}

// NewFanoutClient creates a FanoutClient for the provided webhook base addresses.
func NewFanoutClient(baseAddrs []string, opts ...FanoutOption) (*FanoutClient, error) {
	if len(baseAddrs) == 0 {
		return nil, errors.New("at least one notification endpoint is required")
	}

	endpoints, err := ResolveBaseEndpoints(baseAddrs)
	if err != nil {
		return nil, err
	}

	cfg := &fanoutConfig{
		requestTimeout:     DefaultRequestTimeout,
		drainTimeout:       DefaultDrainTimeout,
		workersPerEndpoint: DefaultWorkersPerEndpoint,
		maxQueueItems:      DefaultMaxQueueItems,
		maxQueueBytes:      DefaultMaxQueueBytes,
		maxItemAge:         DefaultMaxItemAge,
		initialBackoff:     DefaultInitialBackoff,
		maxBackoff:         DefaultMaxBackoff,
		maxRetries:         DefaultMaxRetries,
		clock:              clock.RealClock{},
		jitterFn:           defaultJitter,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	health := newRuleHealthTracker()
	queues := make([]*endpointQueue, len(endpoints))
	for i, ep := range endpoints {
		epSender := cfg.sender
		var ownClient *Client
		if epSender == nil {
			httpCli, err := NewClientWithConcurrency(cfg.requestTimeout, cfg.workersPerEndpoint)
			if err != nil {
				return nil, err
			}
			epSender = httpCli
			ownClient = httpCli
		}
		queues[i] = newEndpointQueue(ep, rawMatchingEndpoint(baseAddrs, ep), epSender, ownClient, cfg, health)
		metrics.EndpointQueueDepth.WithLabelValues(ep.ID).Set(0)
	}

	return &FanoutClient{
		queues:       queues,
		drainTimeout: cfg.drainTimeout,
		clock:        cfg.clock,
	}, nil
}

func rawMatchingEndpoint(rawAddrs []string, target Endpoint) []string {
	var matches []string
	for _, raw := range rawAddrs {
		ep, err := ParseBaseEndpoint(raw)
		if err == nil && ep.URL == target.URL {
			matches = append(matches, strings.TrimSpace(raw))
		}
	}
	return matches
}

// EndpointIDs returns the redacted endpoint identifiers in configured deduplicated order.
func (f *FanoutClient) EndpointIDs() []string {
	out := make([]string, len(f.queues))
	for i, q := range f.queues {
		out[i] = q.endpoint.ID
	}
	return out
}

// Endpoints returns the validated Endpoint descriptors in configured deduplicated order.
func (f *FanoutClient) Endpoints() []Endpoint {
	out := make([]Endpoint, len(f.queues))
	for i, q := range f.queues {
		out[i] = q.endpoint
	}
	return out
}

// Open starts the isolated per-endpoint worker pools on an independent lifecycle context
// so parent shutdown signal cancellation does not abort in-flight deliveries before Close drains.
func (f *FanoutClient) Open(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return ErrClientClosed
	}
	if f.opened {
		return nil
	}

	f.workerCtx, f.workerCancel = context.WithCancel(context.Background())
	f.opened = true

	for _, q := range f.queues {
		for range q.workers {
			f.wg.Add(1)
			go func(eq *endpointQueue, wCtx context.Context) {
				defer f.wg.Done()
				eq.runWorker(wCtx)
			}(q, f.workerCtx)
		}
	}
	return nil
}

// Closed reports whether shutdown drain has begun and new notifications are no longer accepted.
func (f *FanoutClient) Closed() bool {
	f.mu.Lock()
	closed := f.closed
	f.mu.Unlock()
	return closed
}

// Close stops accepting new notifications and drains queued and in-flight deliveries up to drainTimeout
// (default 50s) before cancelling remaining workers and recording shutdown drops.
func (f *FanoutClient) Close() error {
	f.mu.Lock()
	if f.closeStarted {
		f.mu.Unlock()
		return nil
	}
	f.closeStarted = true
	opened := f.opened
	workerCancel := f.workerCancel
	f.mu.Unlock()

	if !opened {
		f.mu.Lock()
		f.closed = true
		f.mu.Unlock()
		for _, q := range f.queues {
			q.beginClose()
			q.dropRemainingOnShutdown()
			q.closeIdleConnections()
		}
		return nil
	}

	drainTimer := f.clock.NewTimer(f.drainTimeout)
	defer drainTimer.Stop()

	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()

	for _, q := range f.queues {
		q.beginClose()
	}

	done := make(chan struct{})
	go func() {
		f.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		if workerCancel != nil {
			workerCancel()
		}
	case <-drainTimer.C():
		if workerCancel != nil {
			workerCancel()
		}
		<-done
		for _, q := range f.queues {
			q.dropRemainingOnShutdown()
		}
	}
	for _, q := range f.queues {
		q.closeIdleConnections()
	}
	return nil
}

// Create enqueues a copy of alert into every configured endpoint's bounded queue.
func (f *FanoutClient) Create(ctx context.Context, _ string, a Alert) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	closed := f.closed
	f.mu.Unlock()
	if closed {
		return ErrClientClosed
	}

	sizeBytes := measureAlertBytes(a)
	for _, q := range f.queues {
		if err := q.enqueueEval(0, a, sizeBytes); err != nil {
			return err
		}
	}
	return nil
}

// BeginEvaluation allocates an evaluation ID and registers its metadata on every endpoint queue.
func (f *FanoutClient) BeginEvaluation(meta EvalMeta) uint64 {
	evalID := f.nextEvalID.Add(1)
	for _, q := range f.queues {
		q.beginEvaluation(evalID, meta)
	}
	return evalID
}

// EnqueueEvaluation enqueues an evaluation row alert across all configured endpoints,
// suppressing it on any endpoint that has already received HTTP 429 for evalID.
func (f *FanoutClient) EnqueueEvaluation(ctx context.Context, evalID uint64, a Alert) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	closed := f.closed
	f.mu.Unlock()
	if closed {
		return ErrClientClosed
	}

	sizeBytes := measureAlertBytes(a)
	for _, q := range f.queues {
		if err := q.enqueueEval(evalID, a, sizeBytes); err != nil {
			return err
		}
	}
	return nil
}

// SealEvaluation marks the producer side of evalID complete on every endpoint.
// If forceThrottled is true (e.g. Kusto row-cap overflow), all endpoints mark evalID throttled
// and merge extraSuppressed into their summary. Once sealed && active == 0 on a throttled endpoint,
// exact-1 notification-failure/<ns>/<name> summary POST is enqueued to that endpoint.
func (f *FanoutClient) SealEvaluation(evalID uint64, forceThrottled bool, extraSuppressed []SuppressedAlert, extraSuppressedTotal int, incomplete bool) {
	for _, q := range f.queues {
		q.sealEvaluation(evalID, forceThrottled, extraSuppressed, extraSuppressedTotal, incomplete)
	}
}

type queuedItem struct {
	alert      Alert
	rule       ruleKey
	evalID     uint64
	isSummary  bool
	sizeBytes  int
	enqueuedAt time.Time
	deadline   time.Time
}

type epEvalState struct {
	meta              EvalMeta
	active            int
	endpointThrottled bool
	summaryRequired   bool
	sealed            bool
	summarySent       bool
	incomplete        bool
	suppressedTotal   int
	suppressed        []SuppressedAlert
}

func (st *epEvalState) addSuppressed(a Alert) {
	st.suppressedTotal++
	if len(st.suppressed) < maxSuppressedSummaryDetails {
		st.suppressed = append(st.suppressed, SuppressedAlert{
			Title:    a.Title,
			Severity: a.Severity,
		})
	}
}

func (st *epEvalState) mergeExtra(extra []SuppressedAlert, extraTotal int) {
	if extraTotal <= 0 && len(extra) == 0 {
		return
	}
	if extraTotal < len(extra) {
		extraTotal = len(extra)
	}
	st.suppressedTotal += extraTotal
	remaining := maxSuppressedSummaryDetails - len(st.suppressed)
	if remaining > 0 && len(extra) > 0 {
		st.suppressed = append(st.suppressed, extra[:min(remaining, len(extra))]...)
	}
}

type endpointQueue struct {
	endpoint       Endpoint
	rawBases       []string
	replacer       *strings.Replacer
	sender         Sender
	ownClient      *Client
	workers        int
	requestTimeout time.Duration
	maxQueueItems  int
	maxQueueBytes  int
	maxItemAge     time.Duration
	initialBackoff time.Duration
	maxBackoff     time.Duration
	maxRetries     int
	clock          clock.Clock
	jitterFn       func(base time.Duration) time.Duration
	health         *ruleHealthTracker

	mu          sync.Mutex
	queue       []queuedItem
	head        int
	queuedBytes int
	closing     bool
	evalStates  map[uint64]*epEvalState

	notifyCh  chan struct{}
	closingCh chan struct{}
}

func newEndpointQueue(ep Endpoint, rawBases []string, sender Sender, ownClient *Client, cfg *fanoutConfig, health *ruleHealthTracker) *endpointQueue {
	allURLs := make([]string, 0, len(rawBases)+1)
	allURLs = append(allURLs, ep.URL)
	allURLs = append(allURLs, rawBases...)
	replacer, _ := buildURLReplacer(allURLs...)
	return &endpointQueue{
		endpoint:       ep,
		rawBases:       rawBases,
		replacer:       replacer,
		sender:         sender,
		ownClient:      ownClient,
		workers:        cfg.workersPerEndpoint,
		requestTimeout: cfg.requestTimeout,
		maxQueueItems:  cfg.maxQueueItems,
		maxQueueBytes:  cfg.maxQueueBytes,
		maxItemAge:     cfg.maxItemAge,
		initialBackoff: cfg.initialBackoff,
		maxBackoff:     cfg.maxBackoff,
		maxRetries:     cfg.maxRetries,
		clock:          cfg.clock,
		jitterFn:       cfg.jitterFn,
		health:         health,
		evalStates:     make(map[uint64]*epEvalState),
		notifyCh:       make(chan struct{}, 1),
		closingCh:      make(chan struct{}),
	}
}

func (q *endpointQueue) closeIdleConnections() {
	if q.ownClient != nil {
		q.ownClient.CloseIdleConnections()
	}
}

func (q *endpointQueue) queueLenLocked() int {
	return len(q.queue) - q.head
}

func (q *endpointQueue) updateDepthGaugeLocked() {
	metrics.EndpointQueueDepth.WithLabelValues(q.endpoint.ID).Set(float64(q.queueLenLocked()))
}

func (q *endpointQueue) beginEvaluation(evalID uint64, meta EvalMeta) {
	q.mu.Lock()
	if !q.closing {
		q.evalStates[evalID] = &epEvalState{meta: meta}
	}
	q.mu.Unlock()
}

func (q *endpointQueue) enqueueEval(evalID uint64, a Alert, sizeBytes int) error {
	q.mu.Lock()
	if q.closing {
		q.mu.Unlock()
		return ErrClientClosed
	}

	var rk ruleKey
	if evalID != 0 {
		if st, ok := q.evalStates[evalID]; ok {
			rk = ruleKey{namespace: st.meta.Namespace, name: st.meta.Name}
			if st.endpointThrottled {
				st.addSuppressed(a)
				q.mu.Unlock()
				q.recordDrop(rk, DropReasonThrottled, false)
				return nil
			}
			st.active++
		}
	}
	if rk.namespace == "" {
		rk = parseRuleFromSource(a.Source)
	}

	now := q.clock.Now()
	item := queuedItem{
		alert:      cloneAlert(a),
		rule:       rk,
		evalID:     evalID,
		sizeBytes:  sizeBytes,
		enqueuedAt: now,
		deadline:   now.Add(q.maxItemAge),
	}
	evicted, anyQueued := q.enqueueItemsLocked([]queuedItem{item})
	q.mu.Unlock()

	for _, ev := range evicted {
		q.logAndCountOverflowDrop(ev)
	}
	if anyQueued {
		q.signalNotify()
	}
	return nil
}

// enqueueItemsLocked iteratively appends items (and any summary alerts unlocked if an evicted item
// drops a sealed throttled evaluation's active count to zero) while enforcing maxQueueItems and
// maxQueueBytes by dropping the oldest queued items without cancelling in-flight HTTP attempts.
// Must be called with q.mu held.
func (q *endpointQueue) enqueueItemsLocked(pending []queuedItem) (evicted []queuedItem, anyQueued bool) {
	for len(pending) > 0 {
		item := pending[0]
		pending = pending[1:]

		for q.queueLenLocked() > 0 && (q.queueLenLocked() >= q.maxQueueItems || q.queuedBytes+item.sizeBytes > q.maxQueueBytes) {
			dropped := q.popHeadRawLocked()
			evicted = append(evicted, dropped)
			if summary := q.decrementEvalOnlyLocked(dropped.evalID); summary != nil {
				pending = append(pending, *summary)
			}
		}

		if q.queueLenLocked() == 0 && item.sizeBytes > q.maxQueueBytes {
			evicted = append(evicted, item)
			if summary := q.decrementEvalOnlyLocked(item.evalID); summary != nil {
				pending = append(pending, *summary)
			}
			continue
		}

		if q.head > 0 && (q.head >= 64 || q.head*2 >= len(q.queue)) {
			n := copy(q.queue, q.queue[q.head:])
			clear(q.queue[n:])
			q.queue = q.queue[:n]
			q.head = 0
		}
		q.queue = append(q.queue, item)
		q.queuedBytes += item.sizeBytes
		anyQueued = true
	}
	q.updateDepthGaugeLocked()
	return evicted, anyQueued
}

func (q *endpointQueue) popHeadRawLocked() queuedItem {
	item := q.queue[q.head]
	q.queue[q.head] = queuedItem{}
	q.head++
	q.queuedBytes -= item.sizeBytes
	if q.head == len(q.queue) {
		q.queue = q.queue[:0]
		q.head = 0
		q.queuedBytes = 0
	}
	return item
}

func (q *endpointQueue) logAndCountOverflowDrop(dropped queuedItem) {
	q.recordDrop(dropped.rule, DropReasonOverflow, true)
	logger.Warnf("Notification queue overflow (maxItems=%d maxBytes=%d) for %s; dropped oldest queued alert %q (source=%s)",
		q.maxQueueItems, q.maxQueueBytes, q.endpoint.ID, dropped.alert.Title, dropped.alert.Source)
}

func (q *endpointQueue) sealEvaluation(evalID uint64, forceThrottled bool, extraSuppressed []SuppressedAlert, extraSuppressedTotal int, incomplete bool) {
	var evicted []queuedItem
	var summaryQueued bool

	q.mu.Lock()
	st, ok := q.evalStates[evalID]
	if !ok {
		q.mu.Unlock()
		return
	}
	if forceThrottled {
		// Row-cap overflow requires a summary once already-enqueued allowed rows finish,
		// without discarding those allowed rows on healthy endpoints.
		st.summaryRequired = true
	}
	if incomplete {
		st.incomplete = true
	}
	st.mergeExtra(extraSuppressed, extraSuppressedTotal)
	st.sealed = true
	evicted, summaryQueued = q.maybeEmitEvalSummaryLocked(evalID, st)
	q.mu.Unlock()

	for _, ev := range evicted {
		q.logAndCountOverflowDrop(ev)
	}
	if summaryQueued {
		q.signalNotify()
	}
}

func (q *endpointQueue) decrementEvalOnlyLocked(evalID uint64) *queuedItem {
	if evalID == 0 {
		return nil
	}
	st, ok := q.evalStates[evalID]
	if !ok {
		return nil
	}
	if st.active > 0 {
		st.active--
	}
	return q.prepareEvalSummaryLocked(evalID, st)
}

func (q *endpointQueue) decrementEvalActiveLocked(evalID uint64) (evicted []queuedItem, summaryQueued bool) {
	summary := q.decrementEvalOnlyLocked(evalID)
	if summary == nil {
		return nil, false
	}
	return q.enqueueItemsLocked([]queuedItem{*summary})
}

func (q *endpointQueue) maybeEmitEvalSummaryLocked(evalID uint64, st *epEvalState) (evicted []queuedItem, summaryQueued bool) {
	summary := q.prepareEvalSummaryLocked(evalID, st)
	if summary == nil {
		return nil, false
	}
	return q.enqueueItemsLocked([]queuedItem{*summary})
}

func (q *endpointQueue) prepareEvalSummaryLocked(evalID uint64, st *epEvalState) *queuedItem {
	if !st.sealed || st.active != 0 {
		return nil
	}
	delete(q.evalStates, evalID)
	if !st.summaryRequired || st.summarySent {
		return nil
	}
	st.summarySent = true

	summaryAlert := buildEvalSummaryAlert(st.meta, st.suppressed, st.suppressedTotal, st.incomplete)
	now := q.clock.Now()
	return &queuedItem{
		alert:      cloneAlert(summaryAlert),
		rule:       ruleKey{namespace: st.meta.Namespace, name: st.meta.Name},
		evalID:     0,
		isSummary:  true,
		sizeBytes:  measureAlertBytes(summaryAlert),
		enqueuedAt: now,
		deadline:   now.Add(q.maxItemAge),
	}
}

func buildEvalSummaryAlert(meta EvalMeta, suppressed []SuppressedAlert, totalSuppressed int, incomplete bool) Alert {
	if meta.BuildSummaryAlert != nil {
		return meta.BuildSummaryAlert(suppressed, totalSuppressed, incomplete)
	}
	source := fmt.Sprintf("notification-failure/%s/%s", meta.Namespace, meta.Name)
	return Alert{
		Destination:   meta.Destination,
		Title:         fmt.Sprintf("Alert %s/%s has too many notifications in %s", meta.Namespace, meta.Name, meta.Region),
		Summary:       FormatThrottledSummary(suppressed, totalSuppressed, incomplete),
		Severity:      3,
		Source:        source,
		CorrelationID: source,
	}
}

// FormatThrottledSummary renders the HTML summary table for suppressed notifications.
func FormatThrottledSummary(suppressed []SuppressedAlert, totalSuppressed int, incomplete bool) string {
	var summary strings.Builder
	summary.WriteString("This alert has been throttled by ADX-Mon due to too many notifications. Please reduce the number of notifications for this alert.")
	if len(suppressed) > 0 {
		summary.WriteString("<br/><br/>Alerts suppressed by throttling:<br/><table><thead><tr><th>Severity</th><th>Title</th></tr></thead><tbody>")
		for _, n := range suppressed {
			fmt.Fprintf(&summary, "<tr><td>%d</td><td>%s</td></tr>", n.Severity, html.EscapeString(n.Title))
		}
		summary.WriteString("</tbody></table>")
	}
	if omitted := totalSuppressed - len(suppressed); omitted > 0 {
		fmt.Fprintf(&summary, "<br/>%d additional suppressed alerts are not shown. Use the query below to view all results.", omitted)
	}
	if incomplete {
		summary.WriteString("<br/>The query results could not be fully processed; this table may be incomplete.")
	}
	return summary.String()
}

func (q *endpointQueue) beginClose() {
	var evicted []queuedItem
	var summaryQueued bool

	q.mu.Lock()
	if q.closing {
		q.mu.Unlock()
		return
	}
	q.closing = true
	// Seal any unsealed evaluations so they do not hang waiting for a stopped producer.
	for evalID, st := range q.evalStates {
		if !st.sealed {
			st.sealed = true
			ev, sq := q.maybeEmitEvalSummaryLocked(evalID, st)
			evicted = append(evicted, ev...)
			if sq {
				summaryQueued = true
			}
		}
	}
	close(q.closingCh)
	q.mu.Unlock()

	for _, ev := range evicted {
		q.logAndCountOverflowDrop(ev)
	}
	if summaryQueued {
		q.signalNotify()
	}
}

func (q *endpointQueue) dropRemainingOnShutdown() {
	q.mu.Lock()
	var remaining []queuedItem
	for q.queueLenLocked() > 0 {
		remaining = append(remaining, q.popHeadRawLocked())
	}
	clear(q.evalStates)
	q.updateDepthGaugeLocked()
	q.mu.Unlock()

	for _, item := range remaining {
		q.recordDrop(item.rule, DropReasonShutdown, false)
	}
}

func (q *endpointQueue) signalNotify() {
	select {
	case q.notifyCh <- struct{}{}:
	default:
	}
}

func (q *endpointQueue) runWorker(workerCtx context.Context) {
	for {
		item, ok := q.nextItem(workerCtx)
		if !ok {
			return
		}
		q.deliverItem(workerCtx, item)
	}
}

type popOutcome struct {
	rule   ruleKey
	reason string
	health bool
}

func (q *endpointQueue) nextItem(workerCtx context.Context) (queuedItem, bool) {
	for {
		if workerCtx.Err() != nil {
			return queuedItem{}, false
		}

		var skipped []popOutcome
		var evicted []queuedItem
		var wakeMore bool
		var chosen queuedItem
		var hasChosen bool
		var shouldExit bool

		q.mu.Lock()
		now := q.clock.Now()
		for q.queueLenLocked() > 0 {
			cand := q.popHeadRawLocked()
			if cand.evalID != 0 {
				if st, ok := q.evalStates[cand.evalID]; ok && st.endpointThrottled {
					st.addSuppressed(cand.alert)
					ev, sq := q.decrementEvalActiveLocked(cand.evalID)
					evicted = append(evicted, ev...)
					if sq {
						wakeMore = true
					}
					skipped = append(skipped, popOutcome{rule: cand.rule, reason: DropReasonThrottled, health: false})
					continue
				}
			}
			if !now.Before(cand.deadline) {
				ev, sq := q.decrementEvalActiveLocked(cand.evalID)
				evicted = append(evicted, ev...)
				if sq {
					wakeMore = true
				}
				skipped = append(skipped, popOutcome{rule: cand.rule, reason: DropReasonTTLExpired, health: true})
				continue
			}
			chosen = cand
			hasChosen = true
			break
		}
		q.updateDepthGaugeLocked()
		if q.queueLenLocked() > 0 {
			wakeMore = true
		}
		if !hasChosen && q.closing {
			shouldExit = true
		}
		q.mu.Unlock()

		for _, s := range skipped {
			q.recordDrop(s.rule, s.reason, s.health)
		}
		for _, ev := range evicted {
			q.logAndCountOverflowDrop(ev)
		}
		if wakeMore {
			q.signalNotify()
		}
		if hasChosen {
			return chosen, true
		}
		if shouldExit {
			return queuedItem{}, false
		}

		select {
		case <-workerCtx.Done():
			return queuedItem{}, false
		case <-q.closingCh:
		case <-q.notifyCh:
		}
	}
}

func (q *endpointQueue) deliverItem(workerCtx context.Context, item queuedItem) {
	for attempt := 0; ; attempt++ {
		if workerCtx.Err() != nil {
			q.finishItem(item, DropReasonShutdown, false)
			return
		}

		if item.evalID != 0 && q.isEvalThrottled(item.evalID) {
			q.recordEvalSuppressedInFlight(item)
			return
		}

		if !q.clock.Now().Before(item.deadline) {
			logger.Warnf("Dropping expired alert %q (source=%s) for %s after exceeding %s TTL",
				item.alert.Title, item.alert.Source, q.endpoint.ID, q.maxItemAge)
			q.finishItem(item, DropReasonTTLExpired, true)
			return
		}

		err, timedOut := q.sendOnce(workerCtx, item.alert)
		if err == nil {
			metrics.NotificationsSentTotal.WithLabelValues(q.endpoint.ID, OutcomeSuccess).Inc()
			q.health.recordSuccess(item.rule, q.endpoint.ID)
			q.finishEvalRef(item.evalID)
			return
		}

		if workerCtx.Err() != nil && !timedOut {
			q.finishItem(item, DropReasonShutdown, false)
			return
		}

		if errors.Is(err, ErrTooManyRequests) {
			metrics.NotificationsSentTotal.WithLabelValues(q.endpoint.ID, OutcomeThrottled).Inc()
			logger.Warnf("Notification throttled (HTTP 429) for alert %q (source=%s) on %s",
				item.alert.Title, item.alert.Source, q.endpoint.ID)
			q.handleHTTP429(item)
			return
		}

		redactedErr := q.redactErr(err)
		if !isRetryableDeliveryError(err, timedOut) {
			metrics.NotificationsSentTotal.WithLabelValues(q.endpoint.ID, OutcomePermanentError).Inc()
			logger.Errorf("Permanent failure delivering alert %q (source=%s) to %s: %s",
				item.alert.Title, item.alert.Source, q.endpoint.ID, redactedErr)
			q.finishItem(item, DropReasonPermanentError, true)
			return
		}

		if attempt >= q.maxRetries {
			metrics.NotificationsSentTotal.WithLabelValues(q.endpoint.ID, OutcomePermanentError).Inc()
			logger.Errorf("Exhausted %d retries delivering alert %q (source=%s) to %s: %s",
				q.maxRetries, item.alert.Title, item.alert.Source, q.endpoint.ID, redactedErr)
			q.finishItem(item, DropReasonRetriesExhausted, true)
			return
		}

		remainingTTL := item.deadline.Sub(q.clock.Now())
		if remainingTTL <= 0 {
			metrics.NotificationsSentTotal.WithLabelValues(q.endpoint.ID, OutcomePermanentError).Inc()
			logger.Warnf("Dropping alert %q (source=%s) for %s after TTL expired (%s): %s",
				item.alert.Title, item.alert.Source, q.endpoint.ID, q.maxItemAge, redactedErr)
			q.finishItem(item, DropReasonTTLExpired, true)
			return
		}

		metrics.NotificationsSentTotal.WithLabelValues(q.endpoint.ID, OutcomeRetry).Inc()
		delay := q.jitteredBackoff(attempt)
		sleepDur := min(delay, remainingTTL)
		logger.Warnf("Transient error delivering alert %q (source=%s) to %s (attempt %d/%d), retrying in %s: %s",
			item.alert.Title, item.alert.Source, q.endpoint.ID, attempt+1, q.maxRetries+1, sleepDur, redactedErr)

		timer := q.clock.NewTimer(sleepDur)
		select {
		case <-workerCtx.Done():
			timer.Stop()
			q.finishItem(item, DropReasonShutdown, false)
			return
		case <-timer.C():
		}
	}
}

func (q *endpointQueue) sendOnce(workerCtx context.Context, a Alert) (err error, timedOut bool) {
	attemptCtx, attemptCancel := context.WithCancel(workerCtx)
	var didTimeout atomic.Bool
	reqTimer := q.clock.NewTimer(q.requestTimeout)
	timerDone := make(chan struct{})
	go func() {
		select {
		case <-reqTimer.C():
			didTimeout.Store(true)
			attemptCancel()
		case <-timerDone:
		}
	}()

	err = q.sender.Create(attemptCtx, q.endpoint.URL, a)
	close(timerDone)
	reqTimer.Stop()
	attemptCancel()
	return err, didTimeout.Load()
}

func (q *endpointQueue) isEvalThrottled(evalID uint64) bool {
	q.mu.Lock()
	st, ok := q.evalStates[evalID]
	throttled := ok && st.endpointThrottled
	q.mu.Unlock()
	return throttled
}

func (q *endpointQueue) recordEvalSuppressedInFlight(item queuedItem) {
	var evicted []queuedItem
	var summaryQueued bool

	q.mu.Lock()
	if st, ok := q.evalStates[item.evalID]; ok {
		st.addSuppressed(item.alert)
	}
	evicted, summaryQueued = q.decrementEvalActiveLocked(item.evalID)
	q.mu.Unlock()

	q.recordDrop(item.rule, DropReasonThrottled, false)
	for _, ev := range evicted {
		q.logAndCountOverflowDrop(ev)
	}
	if summaryQueued {
		q.signalNotify()
	}
}

func (q *endpointQueue) handleHTTP429(item queuedItem) {
	if item.evalID == 0 {
		q.recordDrop(item.rule, DropReasonThrottled, false)
		return
	}

	var sweptCount int
	var evicted []queuedItem
	var summaryQueued bool

	q.mu.Lock()
	if st, ok := q.evalStates[item.evalID]; ok {
		st.endpointThrottled = true
		st.summaryRequired = true
		st.addSuppressed(item.alert)
		sweptCount = q.sweepQueuedEvalItemsLocked(item.evalID, st)
	}
	evicted, summaryQueued = q.decrementEvalActiveLocked(item.evalID)
	q.mu.Unlock()

	for range sweptCount + 1 {
		q.recordDrop(item.rule, DropReasonThrottled, false)
	}
	for _, ev := range evicted {
		q.logAndCountOverflowDrop(ev)
	}
	if summaryQueued {
		q.signalNotify()
	}
}

func (q *endpointQueue) sweepQueuedEvalItemsLocked(evalID uint64, st *epEvalState) int {
	if q.queueLenLocked() == 0 {
		return 0
	}
	writeIdx := q.head
	swept := 0
	for i := q.head; i < len(q.queue); i++ {
		it := q.queue[i]
		if it.evalID == evalID {
			st.addSuppressed(it.alert)
			q.queuedBytes -= it.sizeBytes
			if st.active > 0 {
				st.active--
			}
			swept++
			continue
		}
		q.queue[writeIdx] = it
		writeIdx++
	}
	clear(q.queue[writeIdx:])
	q.queue = q.queue[:writeIdx]
	if q.head == len(q.queue) {
		q.queue = q.queue[:0]
		q.head = 0
		q.queuedBytes = 0
	}
	if swept > 0 {
		q.updateDepthGaugeLocked()
	}
	return swept
}

func (q *endpointQueue) finishItem(item queuedItem, dropReason string, markUnhealthy bool) {
	q.recordDrop(item.rule, dropReason, markUnhealthy)
	q.finishEvalRef(item.evalID)
}

func (q *endpointQueue) finishEvalRef(evalID uint64) {
	if evalID == 0 {
		return
	}
	q.mu.Lock()
	evicted, summaryQueued := q.decrementEvalActiveLocked(evalID)
	q.mu.Unlock()

	for _, ev := range evicted {
		q.logAndCountOverflowDrop(ev)
	}
	if summaryQueued {
		q.signalNotify()
	}
}

func (q *endpointQueue) recordDrop(rk ruleKey, reason string, markUnhealthy bool) {
	metrics.NotificationsDroppedTotal.WithLabelValues(q.endpoint.ID, reason).Inc()
	if rk.namespace != "" && rk.name != "" {
		metrics.RuleNotificationFailuresTotal.WithLabelValues(rk.namespace, rk.name, q.endpoint.ID, reason).Inc()
	}
	if markUnhealthy {
		q.health.recordFailure(rk, q.endpoint.ID)
	}
}

func (q *endpointQueue) baseBackoff(attempt int) time.Duration {
	delay := q.initialBackoff
	for range attempt {
		if delay >= q.maxBackoff/2 {
			return q.maxBackoff
		}
		delay *= 2
	}
	if delay > q.maxBackoff {
		return q.maxBackoff
	}
	return delay
}

func (q *endpointQueue) jitteredBackoff(attempt int) time.Duration {
	base := q.baseBackoff(attempt)
	delay := q.jitterFn(base)
	if delay <= 0 {
		return base
	}
	if delay > q.maxBackoff {
		return q.maxBackoff
	}
	return delay
}

func (q *endpointQueue) redactErr(err error) string {
	if err == nil {
		return ""
	}
	var statusErr *StatusError
	if errors.As(err, &statusErr) {
		return fmt.Sprintf("HTTP %s", localHTTPStatusLine(statusErr.StatusCode))
	}
	var already *redactedError
	if errors.As(err, &already) {
		return err.Error()
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Sprintf("%s %s: %s", ue.Op, q.endpoint.ID, q.replacer.Replace(ue.Err.Error()))
	}
	return q.replacer.Replace(err.Error())
}

type retryClassifier interface {
	Retryable() bool
}

func isRetryableDeliveryError(err error, timedOut bool) bool {
	if errors.Is(err, ErrTooManyRequests) {
		return false
	}
	var rc retryClassifier
	if errors.As(err, &rc) {
		return rc.Retryable()
	}
	if timedOut {
		return true
	}
	return true
}

func measureAlertBytes(a Alert) int {
	b, err := json.Marshal(a)
	if err == nil {
		return max(1, len(b))
	}
	size := len(a.Destination) + len(a.Title) + len(a.Summary) + len(a.Description) + len(a.Source) + len(a.CorrelationID) + 32
	for k, v := range a.CustomFields {
		size += len(k) + len(v) + 4
	}
	return size
}

func parseRuleFromSource(source string) ruleKey {
	trimmed := strings.TrimPrefix(source, "notification-failure/")
	ns, name, ok := strings.Cut(trimmed, "/")
	if !ok || ns == "" || name == "" || strings.Contains(name, "/") {
		return ruleKey{}
	}
	return ruleKey{namespace: ns, name: name}
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
