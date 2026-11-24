package alert_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/metrics"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clocktesting "k8s.io/utils/clock/testing"
)

type proberFunc func(ctx context.Context, endpoint string) (alert.ProbeResponse, error)

func (fn proberFunc) ProbeDestinations(ctx context.Context, endpoint string) (alert.ProbeResponse, error) {
	return fn(ctx, endpoint)
}

func TestParseBaseEndpoint_DestinationsURLPreservesPathAndQuery(t *testing.T) {
	ep, err := alert.ParseBaseEndpoint("https://hooks.example.com/v1/notify,prod?token=secret&env=prod")
	require.NoError(t, err)
	require.Equal(t, "https://hooks.example.com/v1/notify,prod/alerts?token=secret&env=prod", ep.URL)
	require.Equal(t, "https://hooks.example.com/v1/notify,prod/destinations?token=secret&env=prod", ep.DestinationsURL)

	epAlerts, err := alert.ParseBaseEndpoint("https://hooks.example.com/alerts")
	require.NoError(t, err)
	require.Equal(t, "https://hooks.example.com/alerts/alerts", epAlerts.URL)
	require.Equal(t, "https://hooks.example.com/alerts/destinations", epAlerts.DestinationsURL)
}

func TestFanoutClient_SelectiveDiscoveryRoutingAndWireByteIdentity(t *testing.T) {
	type capturedPost struct {
		rawBody []byte
		parsed  alert.Alert
	}

	var mu sync.Mutex
	var catchAllPosts []capturedPost
	var selectivePosts []capturedPost
	var probeHeaders http.Header
	var probeURI string

	srvCatchAll := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.NotFound(w, r)
			return
		}
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var parsed alert.Alert
		require.NoError(t, json.Unmarshal(raw, &parsed))
		mu.Lock()
		catchAllPosts = append(catchAllPosts, capturedPost{rawBody: raw, parsed: parsed})
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	defer srvCatchAll.Close()

	srvSelective := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			mu.Lock()
			probeHeaders = r.Header.Clone()
			probeURI = r.URL.RequestURI()
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"type": "string",
				"anyOf": [
					{"const": "pagerduty"},
					{"pattern": "^(icm|datadog)://"}
				]
			}`))
			return
		}
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var parsed alert.Alert
		require.NoError(t, json.Unmarshal(raw, &parsed))
		mu.Lock()
		selectivePosts = append(selectivePosts, capturedPost{rawBody: raw, parsed: parsed})
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	defer srvSelective.Close()

	fc, err := alert.NewFanoutClient(
		[]string{srvCatchAll.URL, srvSelective.URL + "/base?k=v"},
		alert.WithConcurrency(2),
	)
	require.NoError(t, err)
	require.NoError(t, fc.Open(context.Background()))

	ids := fc.EndpointIDs()
	require.Equal(t, float64(1), metricValue(t, metrics.EndpointDiscoveryHealthy.WithLabelValues(ids[0])))
	require.Equal(t, float64(1), metricValue(t, metrics.EndpointDiscoveryHealthy.WithLabelValues(ids[1])))

	mu.Lock()
	require.Equal(t, "application/json", probeHeaders.Get("Accept"))
	require.Equal(t, "adx-mon-alerter", probeHeaders.Get("User-Agent"))
	require.Equal(t, "/base/destinations?k=v", probeURI)
	mu.Unlock()

	// 1. Legacy singular Destination alert (Destinations nil): wire JSON must be byte-identical to struct without Destinations.
	legacyAlert := alert.Alert{
		Destination:   "icm://compute-oncall",
		Title:         "High CPU",
		Summary:       "CPU > 90%",
		Description:   "Details",
		Severity:      2,
		Source:        "monitoring/high-cpu",
		CorrelationID: "monitoring/high-cpu://node-1",
		CustomFields:  map[string]string{"node": "node-1"},
	}
	type legacyWireAlert struct {
		Destination   string
		Title         string
		Summary       string
		Description   string
		Severity      int
		Source        string
		CorrelationID string
		CustomFields  map[string]string
	}
	expectedLegacyBytes, err := json.Marshal(legacyWireAlert{
		Destination:   legacyAlert.Destination,
		Title:         legacyAlert.Title,
		Summary:       legacyAlert.Summary,
		Description:   legacyAlert.Description,
		Severity:      legacyAlert.Severity,
		Source:        legacyAlert.Source,
		CorrelationID: legacyAlert.CorrelationID,
		CustomFields:  legacyAlert.CustomFields,
	})
	require.NoError(t, err)

	require.NoError(t, fc.Create(context.Background(), "", legacyAlert))

	// 2. Explicit multi-destination alert matching 2 entries on srvSelective -> still exact 1 POST per plugin!
	multiAlert := alert.Alert{
		Destination:   "email://ops",
		Destinations:  []string{"email://ops", "datadog://infra", "icm://compute"},
		Title:         "Disk Full",
		Severity:      3,
		Source:        "monitoring/disk-full",
		CorrelationID: "monitoring/disk-full://vol-1",
	}
	require.NoError(t, fc.Create(context.Background(), "", multiAlert))

	// 3. Explicit alert matching ONLY CatchAll (unmatched on srvSelective).
	emailOnlyAlert := alert.Alert{
		Destination:  "email://ops",
		Destinations: []string{"email://ops"},
		Title:        "Email Only",
		Severity:     4,
		Source:       "monitoring/email-only",
	}
	require.NoError(t, fc.Create(context.Background(), "", emailOnlyAlert))

	require.NoError(t, fc.Close())

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, catchAllPosts, 3, "CatchAll endpoint receives 1 POST per row")
	require.Len(t, selectivePosts, 2, "Selective endpoint receives 1 POST per matched row and skips unmatched row")

	catchAllByTitle := make(map[string]capturedPost, len(catchAllPosts))
	for _, p := range catchAllPosts {
		catchAllByTitle[p.parsed.Title] = p
	}
	selectiveByTitle := make(map[string]capturedPost, len(selectivePosts))
	for _, p := range selectivePosts {
		selectiveByTitle[p.parsed.Title] = p
	}

	// Byte-identical legacy JSON check on both CatchAll and Selective!
	require.Equal(t, string(expectedLegacyBytes), string(catchAllByTitle["High CPU"].rawBody))
	require.Equal(t, string(expectedLegacyBytes), string(selectiveByTitle["High CPU"].rawBody))
	require.NotContains(t, string(catchAllByTitle["High CPU"].rawBody), `"Destinations"`)
	require.NotContains(t, string(selectiveByTitle["High CPU"].rawBody), `"Destinations"`)

	// Explicit destinations: CatchAll gets full ordered list; Selective gets ordered matched subset with matched[0] as Destination!
	require.Equal(t, "email://ops", catchAllByTitle["Disk Full"].parsed.Destination)
	require.Equal(t, []string{"email://ops", "datadog://infra", "icm://compute"}, catchAllByTitle["Disk Full"].parsed.Destinations)

	require.Equal(t, "datadog://infra", selectiveByTitle["Disk Full"].parsed.Destination)
	require.Equal(t, []string{"datadog://infra", "icm://compute"}, selectiveByTitle["Disk Full"].parsed.Destinations)
}

func TestFanoutClient_UnroutedMetricWhenZeroSelectiveEndpointsMatch(t *testing.T) {
	prober := proberFunc(func(_ context.Context, endpoint string) (alert.ProbeResponse, error) {
		if strings.Contains(endpoint, "ep1") {
			return alert.ProbeResponse{
				StatusCode:  http.StatusOK,
				ContentType: "application/json",
				Body:        []byte(`{"const":"icm://team-a"}`),
			}, nil
		}
		return alert.ProbeResponse{
			StatusCode:  http.StatusOK,
			ContentType: "application/json",
			Body:        []byte(`{"const":"pagerduty://team-b"}`),
		}, nil
	})
	var delivered atomic.Int32
	sender := senderFunc(func(_ context.Context, _ string, _ alert.Alert) error {
		delivered.Add(1)
		return nil
	})

	fc, err := alert.NewFanoutClient(
		[]string{"http://ep1.example:8080", "http://ep2.example:8080"},
		alert.WithSender(sender),
		alert.WithDiscoveryProber(prober),
		alert.WithConcurrency(1),
	)
	require.NoError(t, err)
	require.NoError(t, fc.Open(context.Background()))

	ns, name := "unrouted-ns", "unrouted-rule"
	beforeUnrouted := metricValue(t, metrics.NotificationsUnroutedTotal.WithLabelValues(ns, name))

	evalID := fc.BeginEvaluation(alert.EvalMeta{
		RuleUID:      "uid-unrouted",
		Generation:   1,
		Namespace:    ns,
		Name:         name,
		Destination:  "slack://unmatched",
		Destinations: []string{"slack://unmatched"},
		Region:       "eastus",
	})
	require.NoError(t, fc.EnqueueEvaluation(context.Background(), evalID, alert.Alert{
		Destination:  "slack://unmatched",
		Destinations: []string{"slack://unmatched"},
		Title:        "No Receiver Matches",
		Severity:     3,
		Source:       ns + "/" + name,
	}))
	fc.SealEvaluation(evalID, false, nil, 0, false)

	matchReport := fc.EvaluateDestinationMatch([]string{"slack://unmatched"})
	require.Equal(t, metav1.ConditionFalse, matchReport.Status)

	require.NoError(t, fc.Close())
	require.Zero(t, delivered.Load())
	require.Equal(t, beforeUnrouted+1, metricValue(t, metrics.NotificationsUnroutedTotal.WithLabelValues(ns, name)))
}

func TestFanoutClient_DiscoveryStateMachineHysteresisAndFailClosed(t *testing.T) {
	t.Run("3 consecutive non-implemented probes required to downgrade Selective to CatchAll", func(t *testing.T) {
		fakeClock := clocktesting.NewFakeClock(time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC))
		var respMu sync.Mutex
		currentResp := alert.ProbeResponse{
			StatusCode:  http.StatusOK,
			ContentType: "application/json",
			Body:        []byte(`{"const":"icm://only"}`),
		}
		var probeCalls atomic.Int32
		prober := proberFunc(func(_ context.Context, _ string) (alert.ProbeResponse, error) {
			probeCalls.Add(1)
			respMu.Lock()
			defer respMu.Unlock()
			return currentResp, nil
		})

		var deliveredMu sync.Mutex
		var delivered []string
		sender := senderFunc(func(_ context.Context, _ string, a alert.Alert) error {
			deliveredMu.Lock()
			delivered = append(delivered, a.Title)
			deliveredMu.Unlock()
			return nil
		})

		fc, err := alert.NewFanoutClient(
			[]string{"http://hysteresis.example:8080"},
			alert.WithSender(sender),
			alert.WithDiscoveryProber(prober),
			alert.WithClock(fakeClock),
			alert.WithDiscoveryInterval(time.Minute),
			alert.WithDiscoveryJitter(func(d time.Duration) time.Duration { return d }),
			alert.WithConcurrency(1),
		)
		require.NoError(t, err)
		require.NoError(t, fc.Open(context.Background()))
		require.Equal(t, uint64(1), fc.CompletedDiscoveryProbes())
		require.Eventually(t, func() bool { return fc.OnDemandProbeArmedCount() >= 1 }, 2*time.Second, time.Millisecond)
		require.Equal(t, metav1.ConditionFalse, fc.EvaluateDestinationMatch([]string{"other://dest"}).Status)

		// Switch endpoint to 404 Not Found.
		respMu.Lock()
		currentResp = alert.ProbeResponse{StatusCode: http.StatusNotFound}
		respMu.Unlock()

		stepRefresh := func(wantCompleted uint64) {
			t.Helper()
			fakeClock.Step(time.Minute)
			require.Eventually(t, func() bool {
				return fc.CompletedDiscoveryProbes() == wantCompleted
			}, 2*time.Second, time.Millisecond)
		}

		// Probe #1 with 404: still Selective!
		stepRefresh(2)
		require.Equal(t, metav1.ConditionFalse, fc.EvaluateDestinationMatch([]string{"other://dest"}).Status)

		// Probe #2 with 404: STILL Selective!
		stepRefresh(3)
		require.Equal(t, metav1.ConditionFalse, fc.EvaluateDestinationMatch([]string{"other://dest"}).Status)

		// Non-JSON 200 (e.g. proxy HTML error page) when already Selective must NOT downgrade and resets streak!
		respMu.Lock()
		currentResp = alert.ProbeResponse{StatusCode: http.StatusOK, ContentType: "text/html", Body: []byte("<html>proxy</html>")}
		respMu.Unlock()
		stepRefresh(4)
		epID := fc.EndpointIDs()[0]
		require.Equal(t, float64(0), metricValue(t, metrics.EndpointDiscoveryHealthy.WithLabelValues(epID)))
		require.Equal(t, metav1.ConditionFalse, fc.EvaluateDestinationMatch([]string{"other://dest"}).Status)

		// Now 3 consecutive non-implemented probes (404, 405, 501): downgrades on the 3rd!
		for i, code := range []int{http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented} {
			respMu.Lock()
			currentResp = alert.ProbeResponse{StatusCode: code}
			respMu.Unlock()
			stepRefresh(uint64(5 + i))
			if i < 2 {
				require.Equal(t, metav1.ConditionFalse, fc.EvaluateDestinationMatch([]string{"other://dest"}).Status)
			} else {
				require.Equal(t, metav1.ConditionTrue, fc.EvaluateDestinationMatch([]string{"other://dest"}).Status)
			}
		}

		require.NoError(t, fc.Create(context.Background(), "", alert.Alert{
			Destination: "other://dest",
			Title:       "after-downgrade",
			Severity:    3,
			Source:      "ns/r",
		}))
		require.NoError(t, fc.Close())
		deliveredMu.Lock()
		require.Equal(t, []string{"after-downgrade"}, delivered)
		deliveredMu.Unlock()
	})

	t.Run("200 application/json with invalid schema or >64KiB body fails closed and never falls open to CatchAll", func(t *testing.T) {
		fakeClock := clocktesting.NewFakeClock(time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC))
		var respMu sync.Mutex
		// Start with an invalid schema (unknown keyword "not") on initial probe -> never valid!
		currentResp := alert.ProbeResponse{
			StatusCode:  http.StatusOK,
			ContentType: "application/json",
			Body:        []byte(`{"not":{"const":"icm://a"}}`),
		}
		var probeCalls atomic.Int32
		prober := proberFunc(func(_ context.Context, _ string) (alert.ProbeResponse, error) {
			probeCalls.Add(1)
			respMu.Lock()
			defer respMu.Unlock()
			return currentResp, nil
		})

		var postCount atomic.Int32
		sender := senderFunc(func(_ context.Context, _ string, _ alert.Alert) error {
			postCount.Add(1)
			return nil
		})

		fc, err := alert.NewFanoutClient(
			[]string{"http://failclosed.example:8080"},
			alert.WithSender(sender),
			alert.WithDiscoveryProber(prober),
			alert.WithClock(fakeClock),
			alert.WithDiscoveryInterval(time.Minute),
			alert.WithDiscoveryJitter(func(d time.Duration) time.Duration { return d }),
			alert.WithConcurrency(1),
		)
		require.NoError(t, err)
		require.NoError(t, fc.Open(context.Background()))
		require.Eventually(t, func() bool { return fc.OnDemandProbeArmedCount() >= 1 }, 2*time.Second, time.Millisecond)
		epID := fc.EndpointIDs()[0]

		require.Equal(t, float64(0), metricValue(t, metrics.EndpointDiscoveryHealthy.WithLabelValues(epID)))
		require.Equal(t, metav1.ConditionUnknown, fc.EvaluateDestinationMatch([]string{"icm://a"}).Status)

		require.NoError(t, fc.Create(context.Background(), "", alert.Alert{
			Destination: "other://blocked",
			Title:       "must-not-leak",
			Severity:    2,
			Source:      "ns/fc",
		}))
		// Held fail-closed: zero POSTs sent!
		require.Zero(t, postCount.Load())

		// Fix schema to accept only "icm://a", then break it with BodyTooBig (>64 KiB): must retain "icm://a" matcher!
		respMu.Lock()
		currentResp = alert.ProbeResponse{
			StatusCode:  http.StatusOK,
			ContentType: "application/json",
			Body:        []byte(`{"const":"icm://a"}`),
		}
		respMu.Unlock()
		prevCompleted := fc.CompletedDiscoveryProbes()
		fakeClock.Step(time.Minute)
		require.Eventually(t, func() bool {
			return fc.CompletedDiscoveryProbes() > prevCompleted
		}, 2*time.Second, time.Millisecond)
		require.Equal(t, metav1.ConditionTrue, fc.EvaluateDestinationMatch([]string{"icm://a"}).Status)
		// Held "other://blocked" was filtered out upon Selective resolution -> still zero POSTs!
		require.Zero(t, postCount.Load())

		// Now return oversized >64 KiB schema: discovery_healthy=0, retains prior Selective("icm://a") matcher!
		respMu.Lock()
		currentResp = alert.ProbeResponse{
			StatusCode:  http.StatusOK,
			ContentType: "application/json",
			BodyTooBig:  true,
		}
		respMu.Unlock()
		prevCompleted = fc.CompletedDiscoveryProbes()
		fakeClock.Step(time.Minute)
		require.Eventually(t, func() bool {
			return fc.CompletedDiscoveryProbes() > prevCompleted
		}, 2*time.Second, time.Millisecond)
		require.Equal(t, float64(0), metricValue(t, metrics.EndpointDiscoveryHealthy.WithLabelValues(epID)))
		require.Equal(t, metav1.ConditionTrue, fc.EvaluateDestinationMatch([]string{"icm://a"}).Status)
		require.Equal(t, metav1.ConditionFalse, fc.EvaluateDestinationMatch([]string{"other://blocked"}).Status)

		require.NoError(t, fc.Close())
	})
}

func TestFanoutClient_UnknownHoldsInQueueRateLimitsOnDemandProbeAndPreservesAllPostRetries(t *testing.T) {
	fakeClock := clocktesting.NewFakeClock(time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC))
	var probeAttempts atomic.Int32
	var resolveDiscovery atomic.Bool

	prober := proberFunc(func(_ context.Context, _ string) (alert.ProbeResponse, error) {
		probeAttempts.Add(1)
		if !resolveDiscovery.Load() {
			return alert.ProbeResponse{}, errors.New("dial tcp: connection refused")
		}
		return alert.ProbeResponse{
			StatusCode:  http.StatusOK,
			ContentType: "application/json",
			Body:        []byte(`{"pattern":"^icm://"}`),
		}, nil
	})

	var postAttempts atomic.Int32
	postCh := make(chan int32, 8)
	sender := senderFunc(func(_ context.Context, _ string, _ alert.Alert) error {
		n := postAttempts.Add(1)
		postCh <- n
		if n <= 3 {
			// Burn all 3 POST retries after waiting in Unknown to prove 0 retries were consumed while Unknown!
			return &alert.StatusError{StatusCode: http.StatusBadGateway, Status: "502 Bad Gateway"}
		}
		return nil
	})

	fc, err := alert.NewFanoutClient(
		[]string{"http://unknown-hold.example:8080"},
		alert.WithSender(sender),
		alert.WithDiscoveryProber(prober),
		alert.WithClock(fakeClock),
		alert.WithDiscoveryInterval(time.Minute),
		alert.WithDiscoveryJitter(func(d time.Duration) time.Duration { return d }),
		alert.WithBackoff(500*time.Millisecond, 10*time.Second, 3),
		alert.WithJitter(func(d time.Duration) time.Duration { return d }),
		alert.WithConcurrency(1),
	)
	require.NoError(t, err)
	epID := fc.EndpointIDs()[0]

	// Initial probe at Open fails -> endpoint stays Unknown.
	require.NoError(t, fc.Open(context.Background()))
	require.Equal(t, uint64(1), fc.CompletedDiscoveryProbes())
	require.Eventually(t, func() bool { return fc.OnDemandProbeArmedCount() == 1 }, 2*time.Second, time.Millisecond)
	require.Equal(t, int32(1), probeAttempts.Load())
	require.Equal(t, float64(0), metricValue(t, metrics.EndpointDiscoveryHealthy.WithLabelValues(epID)))
	require.Equal(t, metav1.ConditionUnknown, fc.EvaluateDestinationMatch([]string{"icm://team"}).Status)

	ns, name := "hold-ns", "hold-rule"
	beforeUnrouted := metricValue(t, metrics.NotificationsUnroutedTotal.WithLabelValues(ns, name))
	beforeRetry := metricValue(t, metrics.NotificationsSentTotal.WithLabelValues(epID, alert.OutcomeRetry))

	// Enqueue 1 matching alert and 1 non-matching alert rapidly at t=1s (< 5s rate limit).
	fakeClock.Step(time.Second)
	require.NoError(t, fc.Create(context.Background(), "", alert.Alert{
		Destination: "icm://team",
		Title:       "held-matching",
		Severity:    2,
		Source:      ns + "/" + name,
	}))
	require.NoError(t, fc.Create(context.Background(), "", alert.Alert{
		Destination: "slack://other",
		Title:       "held-unmatched",
		Severity:    3,
		Source:      ns + "/" + name,
	}))
	require.Equal(t, float64(2), metricValue(t, metrics.EndpointQueueDepth.WithLabelValues(epID)))
	require.Zero(t, postAttempts.Load(), "Unknown endpoint must not send POSTs before discovery resolves")

	// Wait until runDiscoveryLoop re-arms refreshTimer for the remaining 4s cooldown, then step 3s (t=4s < 5s):
	// must NOT fire a second probe yet!
	require.Eventually(t, func() bool { return fc.OnDemandProbeArmedCount() >= 2 }, 2*time.Second, time.Millisecond)
	fakeClock.Step(3 * time.Second)
	require.Equal(t, uint64(1), fc.CompletedDiscoveryProbes(), "on-demand probe must be rate-limited to at most 1 / 5s")

	// Stepping 1s more reaches t=5s -> rate-limited on-demand probe #2 fires (still fails) and arms next 5s timer.
	fakeClock.Step(time.Second)
	require.Eventually(t, func() bool { return fc.CompletedDiscoveryProbes() == 2 }, 2*time.Second, time.Millisecond)
	require.Zero(t, postAttempts.Load())

	// Stepping another 5s reaches t=10s -> continuous 1/5s on-demand probe #3 fires while queue is non-empty (still fails).
	fakeClock.Step(5 * time.Second)
	require.Eventually(t, func() bool { return fc.CompletedDiscoveryProbes() == 3 }, 2*time.Second, time.Millisecond)
	require.Zero(t, postAttempts.Load())

	// Now allow discovery to resolve on the next 5s on-demand probe (t=15s)!
	resolveDiscovery.Store(true)
	fakeClock.Step(5 * time.Second)
	require.Eventually(t, func() bool { return fc.CompletedDiscoveryProbes() == 4 }, 2*time.Second, time.Millisecond)

	// Matching item enters POST delivery (attempt 1) with all 3 POST retries still intact!
	require.Equal(t, int32(1), recvWithTimeout(t, postCh, "post attempt 1 after Unknown resolved"))
	for idx, step := range []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second} {
		waitForRetryTimer(t, fakeClock, epID, beforeRetry+float64(idx+1))
		fakeClock.Step(step)
		require.Equal(t, int32(idx+2), recvWithTimeout(t, postCh, "post retry attempt"))
	}

	// Once item 1 finishes delivery, the worker pops item 2 ("held-unmatched"), filters it out, and increments unrouted_total!
	require.Eventually(t, func() bool {
		return metricValue(t, metrics.NotificationsUnroutedTotal.WithLabelValues(ns, name)) == beforeUnrouted+1
	}, 2*time.Second, time.Millisecond)

	require.NoError(t, fc.Close())
	require.Equal(t, int32(4), postAttempts.Load())
}

func TestFanoutClient_UnknownExpiresItemsAt5mTTL(t *testing.T) {
	fakeClock := clocktesting.NewFakeClock(time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC))
	prober := proberFunc(func(_ context.Context, _ string) (alert.ProbeResponse, error) {
		return alert.ProbeResponse{StatusCode: http.StatusServiceUnavailable}, nil
	})
	var postCount atomic.Int32
	sender := senderFunc(func(_ context.Context, _ string, _ alert.Alert) error {
		postCount.Add(1)
		return nil
	})

	fc, err := alert.NewFanoutClient(
		[]string{"http://unknown-ttl.example:8080"},
		alert.WithSender(sender),
		alert.WithDiscoveryProber(prober),
		alert.WithClock(fakeClock),
		alert.WithMaxItemAge(5*time.Minute),
		alert.WithConcurrency(1),
	)
	require.NoError(t, err)
	epID := fc.EndpointIDs()[0]
	beforeTTL := metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonTTLExpired))

	require.NoError(t, fc.Open(context.Background()))
	require.Eventually(t, func() bool {
		return fc.OnDemandProbeArmedCount() == 1
	}, 2*time.Second, time.Millisecond)

	meta := alert.RuleMeta{UID: "uid-ttl", Namespace: "ttl-ns", Name: "ttl-rule", Generation: 1}
	require.NoError(t, fc.Create(alert.WithRuleMeta(context.Background(), meta), "", alert.Alert{
		Destination: "icm://team",
		Title:       "expires-in-unknown",
		Severity:    2,
		Source:      "ttl-ns/ttl-rule",
	}))
	require.Eventually(t, func() bool {
		return fc.OnDemandProbeArmedCount() >= 2
	}, 2*time.Second, time.Millisecond)

	// Step past the 5m TTL (first 5s triggers an on-demand probe that still returns 503, then 5m expires TTL).
	fakeClock.Step(5 * time.Second)
	require.Eventually(t, func() bool { return fc.CompletedDiscoveryProbes() >= 2 }, 2*time.Second, time.Millisecond)
	fakeClock.Step(5 * time.Minute)
	require.Eventually(t, func() bool {
		return metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonTTLExpired)) == beforeTTL+1
	}, 2*time.Second, time.Millisecond)
	require.Zero(t, postCount.Load())

	deliv := fc.EvaluateNotificationDelivery(meta)
	require.Equal(t, metav1.ConditionFalse, deliv.Status)
	require.Contains(t, deliv.Message, epID)

	require.NoError(t, fc.Close())
}

func TestDiscoveryRedirectLocationRedaction(t *testing.T) {
	cases := []struct {
		name     string
		location string
		secrets  []string
	}{
		{
			name:     "malformed credential-bearing Location on GET /destinations",
			location: "https://hooks.example.com:SECRET_PORT_TOKEN/dest?sig=SECRET_DEST_SIG",
			secrets:  []string{"SECRET_PORT_TOKEN", "SECRET_DEST_SIG", "hooks.example.com", "BASE_DEST_TOK"},
		},
		{
			name:     "valid credential-bearing Location on GET /destinations to failing target",
			location: "http://rediruser:SECRET_DEST_PASS@127.0.0.1:1/secret-dest-cb?sig=SECRET_DEST_SIG2",
			secrets:  []string{"rediruser", "SECRET_DEST_PASS", "secret-dest-cb", "SECRET_DEST_SIG2", "BASE_DEST_TOK"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logBuf syncBuffer
			prevLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
			defer slog.SetDefault(prevLogger)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", tc.location)
				w.WriteHeader(http.StatusFound)
			}))
			defer srv.Close()

			fc, err := alert.NewFanoutClient(
				[]string{srv.URL + "/base?tok=BASE_DEST_TOK"},
				alert.WithConcurrency(1),
				alert.WithDiscoveryTimeout(500*time.Millisecond),
				alert.WithTimeout(500*time.Millisecond),
				alert.WithBackoff(5*time.Millisecond, 10*time.Millisecond, 1),
			)
			require.NoError(t, err)
			require.NoError(t, fc.Open(context.Background()))

			matchReport := fc.EvaluateDestinationMatch([]string{"icm://team"})
			require.Equal(t, metav1.ConditionUnknown, matchReport.Status)
			require.NotEmpty(t, matchReport.Message)
			require.NoError(t, fc.Close())

			// Also verify NotificationDelivery condition when GET /destinations succeeds (404 CatchAll) and POST /alerts returns 302.
			postSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Location", tc.location)
				w.WriteHeader(http.StatusFound)
			}))
			defer postSrv.Close()

			fcDeliv, err := alert.NewFanoutClient(
				[]string{postSrv.URL + "/base?tok=BASE_DEST_TOK"},
				alert.WithConcurrency(1),
				alert.WithTimeout(500*time.Millisecond),
				alert.WithBackoff(5*time.Millisecond, 10*time.Millisecond, 1),
			)
			require.NoError(t, err)
			require.NoError(t, fcDeliv.Open(context.Background()))

			meta := alert.RuleMeta{UID: "uid-redir", Namespace: "ns", Name: "redir-rule", Generation: 1}
			require.NoError(t, fcDeliv.Create(alert.WithRuleMeta(context.Background(), meta), "", alert.Alert{
				Destination: "icm://team",
				Title:       "redir-check",
				Severity:    2,
				Source:      "ns/redir-rule",
			}))
			require.NoError(t, fcDeliv.Close())

			delivReport := fcDeliv.EvaluateNotificationDelivery(meta)
			require.Equal(t, metav1.ConditionFalse, delivReport.Status)
			require.NotEmpty(t, delivReport.Message)

			for _, sec := range tc.secrets {
				require.NotContains(t, matchReport.Message, sec)
				require.NotContains(t, delivReport.Message, sec)
				require.NotContains(t, logBuf.String(), sec)
			}
		})
	}
}
