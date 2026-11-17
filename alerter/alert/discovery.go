package alert

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"mime"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// DefaultDiscoveryInterval is the periodic capability refresh interval for GET <base>/destinations.
	DefaultDiscoveryInterval = time.Minute

	// DefaultDiscoveryTimeout is the per-probe HTTP GET timeout for capability discovery.
	DefaultDiscoveryTimeout = 10 * time.Second

	// DefaultOnDemandProbeMinInterval rate-limits on-demand probes while an endpoint is Unknown (at most 1 / 5s).
	DefaultOnDemandProbeMinInterval = 5 * time.Second

	// DefaultSelectiveDowngradeThreshold is the number of consecutive non-implemented probes required
	// before downgrading an already-Selective endpoint to CatchAll.
	DefaultSelectiveDowngradeThreshold = 3

	// MaxStatusMessageLength is the maximum byte length written to AlertRule status/condition messages.
	MaxStatusMessageLength = 512
)

// DiscoveryMode represents the capability discovery state of a notification endpoint.
type DiscoveryMode int

const (
	DiscoveryUnknown DiscoveryMode = iota
	DiscoveryCatchAll
	DiscoverySelective
)

// ProbeResponse holds the HTTP response from GET <base>/destinations.
type ProbeResponse struct {
	StatusCode  int
	ContentType string
	Body        []byte
	BodyTooBig  bool
}

// DiscoveryProber probes GET <base>/destinations on a notification endpoint.
type DiscoveryProber interface {
	ProbeDestinations(ctx context.Context, endpoint string) (ProbeResponse, error)
}

type httpDiscoveryProber struct {
	httpClient *http.Client
	maxBytes   int
}

func newHTTPDiscoveryProber(timeout time.Duration) *httpDiscoveryProber {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 4
	t.MaxConnsPerHost = 4
	t.MaxIdleConnsPerHost = 4
	t.ResponseHeaderTimeout = timeout
	t.IdleConnTimeout = time.Minute

	return &httpDiscoveryProber{
		httpClient: &http.Client{
			Transport: t,
		},
		maxBytes: DefaultMaxDiscoveryBodyBytes,
	}
}

func (p *httpDiscoveryProber) CloseIdleConnections() {
	if p != nil && p.httpClient != nil {
		p.httpClient.CloseIdleConnections()
	}
}

func (p *httpDiscoveryProber) ProbeDestinations(ctx context.Context, endpoint string) (ProbeResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ProbeResponse{}, fmt.Errorf("new discovery request: %w", sanitizeHTTPError(err, endpoint))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "adx-mon-alerter")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return ProbeResponse{}, fmt.Errorf("http get destinations: %w", sanitizeHTTPError(err, endpoint))
	}

	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode != http.StatusOK || !isJSONMediaType(ct) {
		resp.Body.Close()
		return ProbeResponse{
			StatusCode:  resp.StatusCode,
			ContentType: ct,
		}, nil
	}

	defer func() {
		io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBodyBytes))
		resp.Body.Close()
	}()

	limit := int64(p.maxBytes) + 1
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, limit))
	if readErr != nil {
		return ProbeResponse{}, fmt.Errorf("read destinations body: %w", sanitizeHTTPError(readErr, endpoint))
	}

	out := ProbeResponse{
		StatusCode:  resp.StatusCode,
		ContentType: ct,
	}
	if len(body) > p.maxBytes {
		out.BodyTooBig = true
	} else {
		out.Body = body
	}
	return out, nil
}

// WithStaticCatchAll pre-marks all endpoints as CatchAll without probing GET <base>/destinations.
// Used for the built-in loopback fakeAlertHandler when --alerter-address is omitted.
func WithStaticCatchAll() FanoutOption {
	return func(cfg *fanoutConfig) {
		cfg.staticCatchAll = true
	}
}

// WithDiscoveryProber overrides the HTTP DiscoveryProber used to query GET <base>/destinations.
func WithDiscoveryProber(prober DiscoveryProber) FanoutOption {
	return func(cfg *fanoutConfig) {
		cfg.discoveryProber = prober
	}
}

// WithDiscoveryInterval overrides the periodic capability discovery refresh interval (default 1m).
func WithDiscoveryInterval(interval time.Duration) FanoutOption {
	return func(cfg *fanoutConfig) {
		if interval > 0 {
			cfg.discoveryInterval = interval
		}
	}
}

// WithDiscoveryTimeout overrides the per-probe HTTP GET timeout for capability discovery (default 10s).
func WithDiscoveryTimeout(timeout time.Duration) FanoutOption {
	return func(cfg *fanoutConfig) {
		if timeout > 0 {
			cfg.discoveryTimeout = timeout
		}
	}
}

// WithDiscoveryJitter overrides the jitter function used for periodic capability discovery refreshes.
func WithDiscoveryJitter(fn func(base time.Duration) time.Duration) FanoutOption {
	return func(cfg *fanoutConfig) {
		if fn != nil {
			cfg.discoveryJitterFn = fn
		}
	}
}

func defaultDiscoveryJitter(base time.Duration) time.Duration {
	if base <= 10 {
		return base
	}
	low := (base * 85) / 100
	high := (base * 115) / 100
	span := int64(high - low)
	if span <= 0 {
		return base
	}
	return low + time.Duration(rand.Int64N(span+1))
}

// RuleMeta identifies an AlertRule instance and generation for latched delivery outcomes and metrics.
type RuleMeta struct {
	UID        string
	Namespace  string
	Name       string
	Generation int64
}

type ruleMetaContextKey struct{}

// WithRuleMeta attaches RuleMeta to ctx for non-evaluation Create calls (such as alert-failure meta-alerts).
func WithRuleMeta(ctx context.Context, meta RuleMeta) context.Context {
	return context.WithValue(ctx, ruleMetaContextKey{}, meta)
}

// RuleMetaFromContext extracts RuleMeta from ctx if present.
func RuleMetaFromContext(ctx context.Context) (RuleMeta, bool) {
	meta, ok := ctx.Value(ruleMetaContextKey{}).(RuleMeta)
	return meta, ok
}

// DestinationMatchReport summarizes whether an AlertRule's destinations match >=1 discovered endpoint.
type DestinationMatchReport struct {
	Status  metav1.ConditionStatus
	Reason  string
	Message string
}

// DeliveryHealthReport summarizes latched asynchronous HTTP delivery outcomes for an AlertRule generation.
type DeliveryHealthReport struct {
	Status  metav1.ConditionStatus
	Reason  string
	Message string
}

// CapStatusMessage truncates s at a valid UTF-8 rune boundary so len(result) <= maxBytes.
func CapStatusMessage(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	if maxBytes <= 3 {
		for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
			maxBytes--
		}
		return s[:maxBytes]
	}
	limit := maxBytes - 3
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit] + "..."
}

// SanitizeMessage strips raw webhook URLs, credentials, paths, and query parameters across all configured
// endpoints and enforces the 512-byte UTF-8 status message cap.
func (f *FanoutClient) SanitizeMessage(msg string) string {
	if msg == "" {
		return ""
	}
	sanitized := msg
	if f != nil && f.replacer != nil {
		sanitized = f.replacer.Replace(sanitized)
	}
	return CapStatusMessage(sanitized, MaxStatusMessageLength)
}

// EvaluateDestinationMatch evaluates an AlertRule's ordered candidate destinations against all configured endpoints.
func (f *FanoutClient) EvaluateDestinationMatch(candidates []string) DestinationMatchReport {
	if len(candidates) == 0 {
		candidates = []string{""}
	}

	var matchedIDs []string
	var unknownIDs []string
	var unknownDetails []string

	for _, q := range f.queues {
		q.mu.Lock()
		mode := q.discoveryMode
		matcher := q.discoveryMatcher
		lastErr := q.lastDiscoveryErr
		q.mu.Unlock()

		switch mode {
		case DiscoveryCatchAll:
			matchedIDs = append(matchedIDs, q.endpoint.ID)
		case DiscoverySelective:
			if matcher != nil && len(matcher.Filter(candidates)) > 0 {
				matchedIDs = append(matchedIDs, q.endpoint.ID)
			}
		default:
			unknownIDs = append(unknownIDs, q.endpoint.ID)
			if lastErr != "" {
				unknownDetails = append(unknownDetails, fmt.Sprintf("%s (%s)", q.endpoint.ID, lastErr))
			}
		}
	}

	if len(matchedIDs) > 0 {
		return DestinationMatchReport{
			Status:  metav1.ConditionTrue,
			Reason:  alertrulev1.ReasonDestinationMatched,
			Message: f.SanitizeMessage(fmt.Sprintf("Matched %d/%d notification endpoints: %s", len(matchedIDs), len(f.queues), strings.Join(matchedIDs, ", "))),
		}
	}

	if len(unknownIDs) > 0 {
		msg := fmt.Sprintf("Waiting for capability discovery on notification endpoints: %s", strings.Join(unknownIDs, ", "))
		if len(unknownDetails) > 0 {
			msg = fmt.Sprintf("Endpoint capability discovery unresolved: %s", strings.Join(unknownDetails, "; "))
		}
		return DestinationMatchReport{
			Status:  metav1.ConditionUnknown,
			Reason:  alertrulev1.ReasonEndpointDiscoveryPending,
			Message: f.SanitizeMessage(msg),
		}
	}

	return DestinationMatchReport{
		Status:  metav1.ConditionFalse,
		Reason:  alertrulev1.ReasonNoMatchingEndpoint,
		Message: f.SanitizeMessage(fmt.Sprintf("Configured destinations did not match any of the %d notification endpoints (%s)", len(f.queues), strings.Join(f.EndpointIDs(), ", "))),
	}
}

// ReconcileLiveRules prunes latched delivery failures and unhealthy gauges for deleted, replaced (new UID),
// or superseded (older generation) AlertRules and prevents late async outcomes for dead UIDs from re-inserting.
func (f *FanoutClient) ReconcileLiveRules(live []RuleMeta) {
	if f == nil || f.health == nil {
		return
	}
	f.health.reconcileLiveRules(live)
}

// RetainedHealthRuleCounts returns the number of rules currently retained in latchedFailures and unhealthy maps.
func (f *FanoutClient) RetainedHealthRuleCounts() (latchedRules int, unhealthyRules int) {
	if f == nil || f.health == nil {
		return 0, 0
	}
	return f.health.retainedCounts()
}

// EvaluateNotificationDelivery inspects latched delivery failures for (ruleUID, generation, endpointID).
func (f *FanoutClient) EvaluateNotificationDelivery(meta RuleMeta) DeliveryHealthReport {
	failures := f.health.snapshotLatchedFailures(meta, f.EndpointIDs())
	if len(failures) == 0 {
		return DeliveryHealthReport{
			Status:  metav1.ConditionTrue,
			Reason:  alertrulev1.ReasonDeliveryHealthy,
			Message: "No notification delivery failures recorded",
		}
	}
	return DeliveryHealthReport{
		Status:  metav1.ConditionFalse,
		Reason:  alertrulev1.ReasonDeliveryFailed,
		Message: f.SanitizeMessage(strings.Join(failures, "; ")),
	}
}

// CompletedDiscoveryProbes returns the total number of completed GET <base>/destinations probe cycles
// across all configured endpoints after each cycle has updated state and re-armed its next refresh timer.
func (f *FanoutClient) CompletedDiscoveryProbes() uint64 {
	var total uint64
	for _, q := range f.queues {
		total += q.completedProbes.Load()
	}
	return total
}

// OnDemandProbeArmedCount returns how many times an Unknown endpoint re-armed its refresh timer
// for the 1/5s on-demand discovery cooldown.
func (f *FanoutClient) OnDemandProbeArmedCount() uint64 {
	var total uint64
	for _, q := range f.queues {
		total += q.onDemandArmed.Load()
	}
	return total
}

func (q *endpointQueue) nextDiscoveryRefreshDelay() (time.Duration, bool) {
	delay := q.discoveryJitterFn(q.discoveryInterval)
	if delay <= 0 {
		delay = q.discoveryInterval
	}
	onDemand := false
	q.mu.Lock()
	if !q.closing && q.discoveryMode == DiscoveryUnknown && q.queueLenLocked() > 0 {
		rem := q.onDemandMinInterval - q.clock.Now().Sub(q.lastProbeStart)
		if rem <= 0 {
			rem = time.Millisecond
		}
		if rem < delay {
			delay = rem
			onDemand = true
		}
	}
	q.mu.Unlock()
	return delay, onDemand
}

func (q *endpointQueue) triggerOnDemandProbe() {
	if q.staticCatchAll {
		return
	}
	select {
	case q.onDemandProbeCh <- struct{}{}:
	default:
	}
}

func stopAndDrainTimer(t interface {
	Stop() bool
	C() <-chan time.Time
}) {
	if !t.Stop() {
		select {
		case <-t.C():
		default:
		}
	}
}

func (q *endpointQueue) runDiscoveryLoop(discoveryCtx context.Context) {
	initDelay, onDemandPending := q.nextDiscoveryRefreshDelay()
	refreshTimer := q.clock.NewTimer(initDelay)
	defer refreshTimer.Stop()
	q.onDemandArmed.Add(1)

	for {
		select {
		case <-discoveryCtx.Done():
			return
		case <-refreshTimer.C():
			onDemandPending = false
			q.probeOnce(discoveryCtx)
			if discoveryCtx.Err() != nil {
				return
			}
			nextDelay, nextOnDemand := q.nextDiscoveryRefreshDelay()
			onDemandPending = nextOnDemand
			refreshTimer.Reset(nextDelay)
			q.completedProbes.Add(1)
		case <-q.onDemandProbeCh:
			if onDemandPending {
				continue
			}
			q.mu.Lock()
			isUnknown := !q.closing && q.discoveryMode == DiscoveryUnknown
			target := q.lastProbeStart.Add(q.onDemandMinInterval)
			q.mu.Unlock()
			if !isUnknown {
				continue
			}

			stopAndDrainTimer(refreshTimer)
			rem := target.Sub(q.clock.Now())
			if rem <= 0 {
				q.probeOnce(discoveryCtx)
				if discoveryCtx.Err() != nil {
					return
				}
				nextDelay, nextOnDemand := q.nextDiscoveryRefreshDelay()
				onDemandPending = nextOnDemand
				refreshTimer.Reset(nextDelay)
				q.completedProbes.Add(1)
			} else {
				onDemandPending = true
				refreshTimer.Reset(rem)
				q.onDemandArmed.Add(1)
			}
		}
	}
}

func (q *endpointQueue) probeOnce(parentCtx context.Context) {
	if q.staticCatchAll || q.prober == nil {
		return
	}
	if parentCtx.Err() != nil {
		return
	}

	q.mu.Lock()
	if q.closing {
		q.mu.Unlock()
		return
	}
	q.lastProbeStart = q.clock.Now()
	q.mu.Unlock()

	probeCtx, probeCancel := context.WithCancel(parentCtx)
	var didTimeout atomic.Bool
	probeTimer := q.clock.NewTimer(q.discoveryTimeout)
	timerDone := make(chan struct{})
	go func() {
		select {
		case <-probeTimer.C():
			didTimeout.Store(true)
			probeCancel()
		case <-timerDone:
		}
	}()

	resp, err := q.prober.ProbeDestinations(probeCtx, q.endpoint.DestinationsURL)
	close(timerDone)
	probeTimer.Stop()
	probeCancel()

	if parentCtx.Err() != nil && !didTimeout.Load() {
		return
	}

	var compiled *Matcher
	var compileErr error
	isJSON := err == nil && isJSONMediaType(resp.ContentType)
	if err == nil && resp.StatusCode == http.StatusOK && isJSON && !resp.BodyTooBig {
		compiled, compileErr = CompileMatcher(resp.Body)
	}

	q.mu.Lock()
	healthy, wakeWorkers, warnMsg := q.applyProbeOutcomeLocked(resp, err, isJSON, compiled, compileErr)
	q.mu.Unlock()

	if healthy {
		metrics.EndpointDiscoveryHealthy.WithLabelValues(q.endpoint.ID).Set(1)
	} else {
		metrics.EndpointDiscoveryHealthy.WithLabelValues(q.endpoint.ID).Set(0)
		if warnMsg != "" {
			logger.Warnf("Endpoint capability discovery unhealthy for %s: %s", q.endpoint.ID, warnMsg)
		}
	}
	if wakeWorkers {
		q.signalNotify()
	}
}

func (q *endpointQueue) applyProbeOutcomeLocked(resp ProbeResponse, probeErr error, isJSON bool, compiled *Matcher, compileErr error) (healthy bool, wakeWorkers bool, warnMsg string) {
	if q.closing {
		return q.discoveryHealthy, false, ""
	}

	if probeErr != nil {
		q.nonImplStreak = 0
		q.discoveryHealthy = false
		redacted := q.redactErr(probeErr)
		q.lastDiscoveryErr = redacted
		return false, false, redacted
	}

	if resp.StatusCode == http.StatusOK && isJSON {
		q.nonImplStreak = 0
		if resp.BodyTooBig {
			q.discoveryHealthy = false
			msg := fmt.Sprintf("destinations schema exceeded %d bytes", DefaultMaxDiscoveryBodyBytes)
			q.lastDiscoveryErr = msg
			if q.discoveryMode != DiscoverySelective || q.discoveryMatcher == nil {
				q.discoveryMode = DiscoveryUnknown
				q.discoveryMatcher = nil
				q.schemaBlocked = true
			}
			return false, false, msg
		}
		if compileErr != nil {
			q.discoveryHealthy = false
			msg := q.replacer.Replace(compileErr.Error())
			q.lastDiscoveryErr = msg
			if q.discoveryMode != DiscoverySelective || q.discoveryMatcher == nil {
				q.discoveryMode = DiscoveryUnknown
				q.discoveryMatcher = nil
				q.schemaBlocked = true
			}
			return false, false, msg
		}

		q.discoveryMode = DiscoverySelective
		q.discoveryMatcher = compiled
		q.schemaBlocked = false
		q.discoveryHealthy = true
		q.lastDiscoveryErr = ""
		return true, q.queueLenLocked() > 0, ""
	}

	if q.isNonImplementedResponseLocked(resp.StatusCode, isJSON) {
		if q.discoveryMode == DiscoverySelective {
			q.nonImplStreak++
			q.discoveryHealthy = true
			q.lastDiscoveryErr = ""
			if q.nonImplStreak >= DefaultSelectiveDowngradeThreshold {
				q.discoveryMode = DiscoveryCatchAll
				q.discoveryMatcher = nil
				q.nonImplStreak = 0
				return true, q.queueLenLocked() > 0, ""
			}
			return true, false, ""
		}

		wasUnknown := q.discoveryMode == DiscoveryUnknown
		q.discoveryMode = DiscoveryCatchAll
		q.discoveryMatcher = nil
		q.schemaBlocked = false
		q.nonImplStreak = 0
		q.discoveryHealthy = true
		q.lastDiscoveryErr = ""
		return true, wasUnknown && q.queueLenLocked() > 0, ""
	}

	q.nonImplStreak = 0
	q.discoveryHealthy = false
	msg := fmt.Sprintf("HTTP %s", localHTTPStatusLine(resp.StatusCode))
	q.lastDiscoveryErr = msg
	return false, false, msg
}

func (q *endpointQueue) isNonImplementedResponseLocked(statusCode int, isJSON bool) bool {
	switch statusCode {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	}
	if q.discoveryMode == DiscoveryUnknown && !q.schemaBlocked {
		switch statusCode {
		case http.StatusCreated, http.StatusNoContent:
			return true
		case http.StatusOK, http.StatusBadRequest, http.StatusForbidden:
			return !isJSON
		}
	}
	return false
}

func isJSONMediaType(contentType string) bool {
	if strings.TrimSpace(contentType) == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return strings.EqualFold(mediaType, "application/json") || strings.EqualFold(mediaType, "application/schema+json")
}

func alertCandidates(a Alert) ([]string, bool) {
	if len(a.Destinations) > 0 {
		return slices.Clone(a.Destinations), true
	}
	return []string{a.Destination}, false
}

func firstOrEmpty(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

func formatAlertForDestinations(base Alert, resolved []string, explicit bool) Alert {
	out := cloneAlert(base)
	out.Destination = firstOrEmpty(resolved)
	if explicit {
		out.Destinations = slices.Clone(resolved)
	} else {
		out.Destinations = nil
	}
	return out
}

type rowRouteTracker struct {
	rule           ruleKey
	trackMetric    bool
	pendingUnknown atomic.Int32
	matchedAny     atomic.Bool
}

func newRowRouteTracker(rk ruleKey, trackMetric bool) *rowRouteTracker {
	t := &rowRouteTracker{
		rule:        rk,
		trackMetric: trackMetric,
	}
	t.pendingUnknown.Store(1)
	return t
}

func (t *rowRouteTracker) retainUnknown() {
	if t != nil {
		t.pendingUnknown.Add(1)
	}
}

func (t *rowRouteTracker) markMatched() {
	if t != nil {
		t.matchedAny.Store(true)
	}
}

func (t *rowRouteTracker) cancelDrop() {
	if t != nil {
		t.matchedAny.Store(true)
		t.releaseOne()
	}
}

func (t *rowRouteTracker) releaseOne() {
	if t == nil {
		return
	}
	if t.pendingUnknown.Add(-1) == 0 && !t.matchedAny.Load() && t.trackMetric {
		if t.rule.namespace != "" && t.rule.name != "" {
			metrics.NotificationsUnroutedTotal.WithLabelValues(t.rule.namespace, t.rule.name).Inc()
		}
	}
}
