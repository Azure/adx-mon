package alert_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/metrics"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/stretchr/testify/require"
	clocktesting "k8s.io/utils/clock/testing"
)

func metricValue(t *testing.T, m prometheus.Metric) float64 {
	t.Helper()
	var pb dto.Metric
	require.NoError(t, m.Write(&pb))
	if pb.Counter != nil {
		return pb.Counter.GetValue()
	}
	if pb.Gauge != nil {
		return pb.Gauge.GetValue()
	}
	return 0
}

func recvWithTimeout[T any](t *testing.T, ch <-chan T, desc string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out after 5s waiting for %s", desc)
		var zero T
		return zero
	}
}

func waitForRetryTimer(t *testing.T, fakeClock *clocktesting.FakeClock, epID string, wantRetryCount float64) {
	t.Helper()
	// OutcomeRetry is incremented only after sendOnce returns and stops its 60s per-request timer,
	// so HasWaiters() alongside OutcomeRetry == wantRetryCount uniquely observes the retry-backoff timer.
	require.Eventually(t, func() bool {
		return metricValue(t, metrics.NotificationsSentTotal.WithLabelValues(epID, alert.OutcomeRetry)) == wantRetryCount &&
			fakeClock.HasWaiters()
	}, 5*time.Second, time.Millisecond)
}

type senderFunc func(ctx context.Context, endpoint string, a alert.Alert) error

func (fn senderFunc) Create(ctx context.Context, endpoint string, a alert.Alert) error {
	return fn(ctx, endpoint, a)
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestParseBaseEndpoint_ValidationJoiningAndDeduplication(t *testing.T) {
	t.Run("structural path join preserves query and commas and never strips /alerts", func(t *testing.T) {
		cases := []struct {
			raw        string
			wantURL    string
			wantPrefix string
		}{
			{
				raw:        "http://notifier:8080",
				wantURL:    "http://notifier:8080/alerts",
				wantPrefix: "http://notifier:8080/#",
			},
			{
				raw:        "http://notifier:8080/",
				wantURL:    "http://notifier:8080/alerts",
				wantPrefix: "http://notifier:8080/#",
			},
			{
				raw:        "https://receiver.example/team,prod?sig=abc&v=1,2",
				wantURL:    "https://receiver.example/team,prod/alerts?sig=abc&v=1,2",
				wantPrefix: "https://receiver.example/#",
			},
			{
				raw:        "https://receiver.example/alerts",
				wantURL:    "https://receiver.example/alerts/alerts",
				wantPrefix: "https://receiver.example/#",
			},
			{
				raw:        "https://user:secret@receiver.example:8443/v1/alerts?token=xyz",
				wantURL:    "https://user:secret@receiver.example:8443/v1/alerts/alerts?token=xyz",
				wantPrefix: "https://receiver.example:8443/#",
			},
			{
				raw:        "http://[::1]:9090/hook",
				wantURL:    "http://[::1]:9090/hook/alerts",
				wantPrefix: "http://[::1]:9090/#",
			},
		}

		for _, tc := range cases {
			ep, err := alert.ParseBaseEndpoint(tc.raw)
			require.NoError(t, err, tc.raw)
			require.Equal(t, tc.wantURL, ep.URL)

			sum := sha256.Sum256([]byte(tc.wantURL))
			wantID := fmt.Sprintf("%s%x", tc.wantPrefix, sum[:4])
			require.Equal(t, wantID, ep.ID)
			require.Equal(t, wantID, alert.RedactURL(ep.URL))
		}
	})

	t.Run("rejects invalid endpoints without leaking raw URL", func(t *testing.T) {
		badInputs := []string{
			"",
			"   ",
			"ftp://user:SECRET@bad.example/hook",
			"file:///etc/passwd",
			"http://",
			"https://:8080/path",
			"notifier:8080",
		}
		for _, bad := range badInputs {
			_, err := alert.ParseBaseEndpoint(bad)
			require.Error(t, err, "expected error for %q", bad)
			if strings.Contains(bad, "SECRET") {
				require.NotContains(t, err.Error(), "SECRET")
				require.NotContains(t, err.Error(), bad)
			}
		}
	})

	t.Run("deduplicates normalized endpoints in order", func(t *testing.T) {
		eps, err := alert.ResolveBaseEndpoints([]string{
			"http://a.example:8080",
			"https://b.example/hook?x=1",
			"http://A.EXAMPLE:8080/",
			"https://b.example/hook/?x=1",
			"http://c.example/alerts",
		})
		require.NoError(t, err)
		require.Len(t, eps, 3)
		require.Equal(t, "http://a.example:8080/alerts", eps[0].URL)
		require.Equal(t, "https://b.example/hook/alerts?x=1", eps[1].URL)
		require.Equal(t, "http://c.example/alerts/alerts", eps[2].URL)
	})
}

func TestClientAndFanout_Strict201AndErrorBodyCap(t *testing.T) {
	t.Run("error body read is capped at 4 KiB and non-201 non-5xx statuses fail permanently without retry", func(t *testing.T) {
		for _, code := range []int{http.StatusOK, http.StatusNoContent, http.StatusBadRequest, http.StatusNotFound} {
			t.Run(fmt.Sprintf("status_%d", code), func(t *testing.T) {
				var hits atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					hits.Add(1)
					w.WriteHeader(code)
					if code != http.StatusNoContent {
						_, _ = w.Write([]byte(strings.Repeat("E", 64*1024)))
					}
				}))
				defer srv.Close()

				rawCli, err := alert.NewClient(5 * time.Second)
				require.NoError(t, err)
				rawErr := rawCli.Create(context.Background(), srv.URL+"/alerts", alert.Alert{
					Title:    "cap-test",
					Severity: 2,
					Source:   "ns/rule-cap",
				})
				require.Error(t, rawErr)
				var statusErr *alert.StatusError
				require.ErrorAs(t, rawErr, &statusErr)
				require.Equal(t, code, statusErr.StatusCode)
				require.False(t, statusErr.Retryable())
				if code != http.StatusNoContent {
					require.Len(t, statusErr.Body, 4*1024)
				}
				hits.Store(0)

				ns := "strict-ns"
				name := fmt.Sprintf("rule-%d", code)
				fc, err := alert.NewFanoutClient(
					[]string{srv.URL},
					alert.WithConcurrency(2),
					alert.WithBackoff(5*time.Millisecond, 20*time.Millisecond, 3),
				)
				require.NoError(t, err)
				epID := fc.EndpointIDs()[0]

				beforePerm := metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonPermanentError))
				beforeRuleFail := metricValue(t, metrics.RuleNotificationFailuresTotal.WithLabelValues(ns, name, epID, alert.DropReasonPermanentError))

				require.NoError(t, fc.Open(context.Background()))
				require.NoError(t, fc.Create(context.Background(), "", alert.Alert{
					Title:    "non-201",
					Severity: 2,
					Source:   ns + "/" + name,
				}))
				require.NoError(t, fc.Close())

				// Must NOT retry on 200, 204, 400, 404: exact 1 attempt.
				require.Equal(t, int32(1), hits.Load())
				require.Equal(t, beforePerm+1, metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonPermanentError)))
				require.Equal(t, beforeRuleFail+1, metricValue(t, metrics.RuleNotificationFailuresTotal.WithLabelValues(ns, name, epID, alert.DropReasonPermanentError)))
				require.Equal(t, float64(1), metricValue(t, metrics.NotificationUnhealthy.WithLabelValues(ns, name)))
			})
		}
	})

	t.Run("NotificationUnhealthy stays 1 while any endpoint fails and clears to 0 once all succeed with 201", func(t *testing.T) {
		var failB atomic.Bool
		failB.Store(true)

		srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
		}))
		defer srvA.Close()

		srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if failB.Load() {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
		}))
		defer srvB.Close()

		fc, err := alert.NewFanoutClient([]string{srvA.URL, srvB.URL}, alert.WithConcurrency(1))
		require.NoError(t, err)
		require.NoError(t, fc.Open(context.Background()))

		ns, name := "health-ns", "multi-ep-rule"
		require.NoError(t, fc.Create(context.Background(), "", alert.Alert{
			Title:    "first",
			Severity: 3,
			Source:   ns + "/" + name,
		}))
		require.Eventually(t, func() bool {
			return metricValue(t, metrics.NotificationUnhealthy.WithLabelValues(ns, name)) == 1
		}, 2*time.Second, 5*time.Millisecond)

		failB.Store(false)
		require.NoError(t, fc.Create(context.Background(), "", alert.Alert{
			Title:    "second",
			Severity: 3,
			Source:   ns + "/" + name,
		}))
		require.Eventually(t, func() bool {
			return metricValue(t, metrics.NotificationUnhealthy.WithLabelValues(ns, name)) == 0
		}, 2*time.Second, 5*time.Millisecond)

		require.NoError(t, fc.Close())
	})
}

func TestFanoutClient_RetriesWithExponentialBackoffJitterAnd5mTTL(t *testing.T) {
	t.Run("retries 5xx and timeout up to 3 retries with exponential backoff and jitter", func(t *testing.T) {
		fakeClock := clocktesting.NewFakeClock(time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC))
		var attempts atomic.Int32
		attemptReady := make(chan int32, 8)
		var jitterBases []time.Duration
		var jitterMu sync.Mutex

		sender := senderFunc(func(ctx context.Context, _ string, _ alert.Alert) error {
			n := attempts.Add(1)
			attemptReady <- n
			if n <= 3 {
				return &alert.StatusError{StatusCode: http.StatusServiceUnavailable, Status: "503 Service Unavailable"}
			}
			return nil
		})

		fc, err := alert.NewFanoutClient(
			[]string{"http://retry.example:8080"},
			alert.WithSender(sender),
			alert.WithConcurrency(1),
			alert.WithClock(fakeClock),
			alert.WithJitter(func(base time.Duration) time.Duration {
				jitterMu.Lock()
				jitterBases = append(jitterBases, base)
				jitterMu.Unlock()
				return base
			}),
		)
		require.NoError(t, err)
		epID := fc.EndpointIDs()[0]
		beforeSuccess := metricValue(t, metrics.NotificationsSentTotal.WithLabelValues(epID, alert.OutcomeSuccess))
		beforeRetry := metricValue(t, metrics.NotificationsSentTotal.WithLabelValues(epID, alert.OutcomeRetry))

		require.NoError(t, fc.Open(context.Background()))
		require.NoError(t, fc.Create(context.Background(), "", alert.Alert{
			Title:    "transient-503",
			Severity: 2,
			Source:   "ns/retry-rule",
		}))

		require.Equal(t, int32(1), recvWithTimeout(t, attemptReady, "attempt 1"))
		for idx, step := range []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second} {
			waitForRetryTimer(t, fakeClock, epID, beforeRetry+float64(idx+1))
			fakeClock.Step(step)
			require.Equal(t, int32(idx+2), recvWithTimeout(t, attemptReady, fmt.Sprintf("attempt %d", idx+2)))
		}

		require.NoError(t, fc.Close())
		require.Equal(t, int32(4), attempts.Load())
		require.Equal(t, []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second}, jitterBases)
		require.Equal(t, beforeSuccess+1, metricValue(t, metrics.NotificationsSentTotal.WithLabelValues(epID, alert.OutcomeSuccess)))
		require.Equal(t, beforeRetry+3, metricValue(t, metrics.NotificationsSentTotal.WithLabelValues(epID, alert.OutcomeRetry)))
	})

	t.Run("exhausts after max 3 retries (4 total attempts)", func(t *testing.T) {
		fakeClock := clocktesting.NewFakeClock(time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC))
		var attempts atomic.Int32
		attemptCh := make(chan int32, 8)

		sender := senderFunc(func(ctx context.Context, _ string, _ alert.Alert) error {
			n := attempts.Add(1)
			attemptCh <- n
			return errors.New("dial tcp: connection reset by peer")
		})

		fc, err := alert.NewFanoutClient(
			[]string{"http://exhaust.example:8080"},
			alert.WithSender(sender),
			alert.WithConcurrency(1),
			alert.WithClock(fakeClock),
			alert.WithJitter(func(base time.Duration) time.Duration { return base }),
		)
		require.NoError(t, err)
		epID := fc.EndpointIDs()[0]
		beforeExhausted := metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonRetriesExhausted))
		beforeRetry := metricValue(t, metrics.NotificationsSentTotal.WithLabelValues(epID, alert.OutcomeRetry))

		require.NoError(t, fc.Open(context.Background()))
		require.NoError(t, fc.Create(context.Background(), "", alert.Alert{
			Title:    "always-fails",
			Severity: 2,
			Source:   "ns/exhaust-rule",
		}))

		require.Equal(t, int32(1), recvWithTimeout(t, attemptCh, "exhaust attempt 1"))
		for idx, step := range []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second} {
			waitForRetryTimer(t, fakeClock, epID, beforeRetry+float64(idx+1))
			fakeClock.Step(step)
			require.Equal(t, int32(idx+2), recvWithTimeout(t, attemptCh, fmt.Sprintf("exhaust attempt %d", idx+2)))
		}

		require.NoError(t, fc.Close())
		require.Equal(t, int32(4), attempts.Load())
		require.Equal(t, beforeExhausted+1, metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonRetriesExhausted)))
	})

	t.Run("60s per-POST timeout retries and 5m TTL drops expired item", func(t *testing.T) {
		fakeClock := clocktesting.NewFakeClock(time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC))
		var attempts atomic.Int32
		started := make(chan int32, 8)

		// Each POST hangs until its 60s attempt context expires.
		sender := senderFunc(func(ctx context.Context, _ string, _ alert.Alert) error {
			n := attempts.Add(1)
			started <- n
			<-ctx.Done()
			return ctx.Err()
		})

		fc, err := alert.NewFanoutClient(
			[]string{"http://ttl.example:8080"},
			alert.WithSender(sender),
			alert.WithConcurrency(1),
			alert.WithClock(fakeClock),
			alert.WithMaxItemAge(130*time.Second), // 2x 60s timeouts + backoff exceeds 130s TTL before 3rd retry
			alert.WithJitter(func(base time.Duration) time.Duration { return base }),
		)
		require.NoError(t, err)
		epID := fc.EndpointIDs()[0]
		beforeTTL := metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonTTLExpired))
		beforeRetry := metricValue(t, metrics.NotificationsSentTotal.WithLabelValues(epID, alert.OutcomeRetry))

		require.NoError(t, fc.Open(context.Background()))
		require.NoError(t, fc.Create(context.Background(), "", alert.Alert{
			Title:    "slow-timeout",
			Severity: 2,
			Source:   "ns/ttl-rule",
		}))

		// Attempt 1 hangs until 60s per-POST timeout (reqTimer is armed before sender.Create is entered)
		require.Equal(t, int32(1), recvWithTimeout(t, started, "ttl attempt 1"))
		require.Eventually(t, fakeClock.HasWaiters, 5*time.Second, time.Millisecond)
		fakeClock.Step(60 * time.Second)
		waitForRetryTimer(t, fakeClock, epID, beforeRetry+1)
		fakeClock.Step(500 * time.Millisecond)

		// Attempt 2 hangs until 60s per-POST timeout (t = 120.5s)
		require.Equal(t, int32(2), recvWithTimeout(t, started, "ttl attempt 2"))
		require.Eventually(t, fakeClock.HasWaiters, 5*time.Second, time.Millisecond)
		fakeClock.Step(60 * time.Second)
		waitForRetryTimer(t, fakeClock, epID, beforeRetry+2)
		fakeClock.Step(time.Second)

		// Attempt 3 hangs until 60s per-POST timeout (t = 181.5s > 130s TTL) -> dropped with ttl_expired
		require.Equal(t, int32(3), recvWithTimeout(t, started, "ttl attempt 3"))
		require.Eventually(t, fakeClock.HasWaiters, 5*time.Second, time.Millisecond)
		fakeClock.Step(60 * time.Second)

		require.NoError(t, fc.Close())
		require.Equal(t, beforeTTL+1, metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonTTLExpired)))
	})
}

func TestFanoutClient_OverflowDropsOldestQueuedWithoutCancellingInFlight(t *testing.T) {
	t.Run("item limit drops oldest queued without cancelling in-flight POST", func(t *testing.T) {
		inFlightStarted := make(chan struct{})
		releaseInFlight := make(chan struct{})
		var inFlightCancelled atomic.Bool

		var mu sync.Mutex
		var delivered []string

		sender := senderFunc(func(ctx context.Context, _ string, a alert.Alert) error {
			if a.Title == "alert-1-inflight" {
				close(inFlightStarted)
				select {
				case <-ctx.Done():
					inFlightCancelled.Store(true)
					return ctx.Err()
				case <-releaseInFlight:
				}
			}
			mu.Lock()
			delivered = append(delivered, a.Title)
			mu.Unlock()
			return nil
		})

		fc, err := alert.NewFanoutClient(
			[]string{"http://overflow-items.example:8080"},
			alert.WithSender(sender),
			alert.WithConcurrency(1),
			alert.WithMaxQueueItems(2),
		)
		require.NoError(t, err)
		epID := fc.EndpointIDs()[0]
		beforeOverflow := metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonOverflow))

		require.NoError(t, fc.Open(context.Background()))

		// Pop alert-1 into in-flight worker
		require.NoError(t, fc.Create(context.Background(), "", alert.Alert{Title: "alert-1-inflight", Severity: 1, Source: "ns/r1"}))
		recvWithTimeout(t, inFlightStarted, "item overflow in-flight start")

		// Enqueue 3 items into maxQueueItems=2 queue: alert-2-oldest is evicted, alert-1-inflight is NOT cancelled!
		require.NoError(t, fc.Create(context.Background(), "", alert.Alert{Title: "alert-2-oldest", Severity: 1, Source: "ns/r1"}))
		require.NoError(t, fc.Create(context.Background(), "", alert.Alert{Title: "alert-3-kept", Severity: 1, Source: "ns/r1"}))
		require.NoError(t, fc.Create(context.Background(), "", alert.Alert{Title: "alert-4-newest", Severity: 1, Source: "ns/r1"}))

		require.False(t, inFlightCancelled.Load())
		close(releaseInFlight)
		require.NoError(t, fc.Close())

		require.False(t, inFlightCancelled.Load())
		require.Equal(t, []string{"alert-1-inflight", "alert-3-kept", "alert-4-newest"}, delivered)
		require.Equal(t, beforeOverflow+1, metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonOverflow)))
	})

	t.Run("byte budget limit drops oldest queued items without cancelling in-flight POST", func(t *testing.T) {
		inFlightStarted := make(chan struct{})
		releaseInFlight := make(chan struct{})
		var mu sync.Mutex
		var delivered []string

		sender := senderFunc(func(ctx context.Context, _ string, a alert.Alert) error {
			if a.Title == "inflight" {
				close(inFlightStarted)
				<-releaseInFlight
			}
			mu.Lock()
			delivered = append(delivered, a.Title)
			mu.Unlock()
			return nil
		})

		sample := alert.Alert{Title: "queued-a", Description: strings.Repeat("x", 200), Severity: 2, Source: "ns/r2"}
		rawJSON, err := json.Marshal(sample)
		require.NoError(t, err)
		// Fit at most 1 such item in the queue by byte budget
		fc, err := alert.NewFanoutClient(
			[]string{"http://overflow-bytes.example:8080"},
			alert.WithSender(sender),
			alert.WithConcurrency(1),
			alert.WithMaxQueueItems(1024),
			alert.WithMaxQueueBytes(len(rawJSON)+20),
		)
		require.NoError(t, err)
		epID := fc.EndpointIDs()[0]
		beforeOverflow := metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonOverflow))

		require.NoError(t, fc.Open(context.Background()))
		require.NoError(t, fc.Create(context.Background(), "", alert.Alert{Title: "inflight", Severity: 2, Source: "ns/r2"}))
		recvWithTimeout(t, inFlightStarted, "byte overflow in-flight start")

		require.NoError(t, fc.Create(context.Background(), "", sample))
		sample2 := sample
		sample2.Title = "queued-b"
		require.NoError(t, fc.Create(context.Background(), "", sample2))

		close(releaseInFlight)
		require.NoError(t, fc.Close())

		require.Equal(t, []string{"inflight", "queued-b"}, delivered)
		require.Equal(t, beforeOverflow+1, metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonOverflow)))
	})
}

func TestFanoutClient_SlowReceiver46sShutdownDrainAnd50sDeadline(t *testing.T) {
	t.Run("46s slow receiver drains cleanly during Close with 0 shutdown drops even after parent ctx cancels", func(t *testing.T) {
		fakeClock := clocktesting.NewFakeClock(time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC))
		inFlightStarted := make(chan struct{})
		releaseReceiver := make(chan struct{})
		var cancelledAt46s atomic.Bool
		var delivered atomic.Int32

		sender := senderFunc(func(ctx context.Context, _ string, a alert.Alert) error {
			if a.Title == "slow-1" {
				close(inFlightStarted)
				select {
				case <-ctx.Done():
					cancelledAt46s.Store(true)
					return ctx.Err()
				case <-releaseReceiver:
				}
			}
			delivered.Add(1)
			return nil
		})

		// Uses unmodified production defaults: DefaultDrainTimeout=50s, DefaultRequestTimeout=60s, DefaultMaxItemAge=5m.
		fc, err := alert.NewFanoutClient(
			[]string{"http://slow-drain-46s.example:8080"},
			alert.WithSender(sender),
			alert.WithConcurrency(1),
			alert.WithClock(fakeClock),
		)
		require.NoError(t, err)
		epID := fc.EndpointIDs()[0]
		beforeShutdownDrops := metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonShutdown))

		parentCtx, parentCancel := context.WithCancel(context.Background())
		require.NoError(t, fc.Open(parentCtx))

		require.NoError(t, fc.Create(context.Background(), "", alert.Alert{Title: "slow-1", Severity: 2, Source: "ns/drain"}))
		require.NoError(t, fc.Create(context.Background(), "", alert.Alert{Title: "queued-2", Severity: 2, Source: "ns/drain"}))
		recvWithTimeout(t, inFlightStarted, "46s slow receiver start")

		// Simulate SIGTERM cancelling parent service context BEFORE Close drains
		parentCancel()

		closeDone := make(chan error, 1)
		go func() {
			closeDone <- fc.Close()
		}()

		// Wait until Close has armed its 50s drain timer (observable via fc.Closed()),
		// then advance 46s (< 50s drain budget)
		require.Eventually(t, fc.Closed, 5*time.Second, time.Millisecond)
		fakeClock.Step(46 * time.Second)

		select {
		case err := <-closeDone:
			t.Fatalf("Close returned prematurely at 46s while slow POST was still in flight: %v", err)
		default:
		}
		require.False(t, cancelledAt46s.Load(), "in-flight POST must not be cancelled at 46s")

		// Slow receiver responds 201 at t=46s; worker also drains queued-2 within remaining 4s budget
		close(releaseReceiver)
		require.NoError(t, recvWithTimeout(t, closeDone, "46s drain Close completion"))

		require.Equal(t, int32(2), delivered.Load())
		require.Equal(t, beforeShutdownDrops, metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonShutdown)))
		require.Equal(t, float64(0), metricValue(t, metrics.EndpointQueueDepth.WithLabelValues(epID)))
	})

	t.Run("exceeding 50s drain deadline cancels stuck worker and records shutdown drops for in-flight and queued items", func(t *testing.T) {
		fakeClock := clocktesting.NewFakeClock(time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC))
		inFlightStarted := make(chan struct{})

		sender := senderFunc(func(ctx context.Context, _ string, _ alert.Alert) error {
			select {
			case <-inFlightStarted:
			default:
				close(inFlightStarted)
			}
			<-ctx.Done()
			return ctx.Err()
		})

		fc, err := alert.NewFanoutClient(
			[]string{"http://hung-drain-50s.example:8080"},
			alert.WithSender(sender),
			alert.WithConcurrency(1),
			alert.WithClock(fakeClock),
		)
		require.NoError(t, err)
		epID := fc.EndpointIDs()[0]
		beforeShutdownDrops := metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonShutdown))

		require.NoError(t, fc.Open(context.Background()))
		require.NoError(t, fc.Create(context.Background(), "", alert.Alert{Title: "hung-1", Severity: 2, Source: "ns/hung"}))
		require.NoError(t, fc.Create(context.Background(), "", alert.Alert{Title: "hung-queued-2", Severity: 2, Source: "ns/hung"}))
		recvWithTimeout(t, inFlightStarted, "50s hung receiver start")

		closeDone := make(chan error, 1)
		go func() {
			closeDone <- fc.Close()
		}()

		require.Eventually(t, fc.Closed, 5*time.Second, time.Millisecond)
		fakeClock.Step(50 * time.Second)
		require.NoError(t, recvWithTimeout(t, closeDone, "50s drain Close completion"))

		require.Equal(t, beforeShutdownDrops+2, metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonShutdown)))
		require.Equal(t, float64(0), metricValue(t, metrics.EndpointQueueDepth.WithLabelValues(epID)))
	})
}

func TestFanoutClient_ConcurrentBurst250AlertsWith429EndpointIsolation(t *testing.T) {
	const (
		numRules    = 10
		rowsPerRule = 25
		totalAlerts = numRules * rowsPerRule
	)

	type endpointRecorder struct {
		mu        sync.Mutex
		regular   []alert.Alert
		summaries []alert.Alert
	}
	newHealthyServer := func(rec *endpointRecorder) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			var a alert.Alert
			if err := json.Unmarshal(body, &a); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			rec.mu.Lock()
			if strings.HasPrefix(a.Source, "notification-failure/") {
				rec.summaries = append(rec.summaries, a)
			} else {
				rec.regular = append(rec.regular, a)
			}
			rec.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		}))
	}

	var recA, recB, recThrottled endpointRecorder
	srvHealthyA := newHealthyServer(&recA)
	defer srvHealthyA.Close()
	srvHealthyB := newHealthyServer(&recB)
	defer srvHealthyB.Close()

	// Throttled endpoint allows the first alert per rule (201) and rejects subsequent regular alerts with HTTP 429,
	// while accepting notification-failure/<ns>/<name> summary alerts with 201 Created.
	var ruleHitsMu sync.Mutex
	ruleHits := make(map[string]int)
	srvThrottled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var a alert.Alert
		if err := json.Unmarshal(body, &a); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if strings.HasPrefix(a.Source, "notification-failure/") {
			recThrottled.mu.Lock()
			recThrottled.summaries = append(recThrottled.summaries, a)
			recThrottled.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			return
		}

		ruleHitsMu.Lock()
		ruleHits[a.Source]++
		hitNum := ruleHits[a.Source]
		ruleHitsMu.Unlock()

		if hitNum > 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		recThrottled.mu.Lock()
		recThrottled.regular = append(recThrottled.regular, a)
		recThrottled.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	defer srvThrottled.Close()

	fc, err := alert.NewFanoutClient(
		[]string{srvHealthyA.URL, srvHealthyB.URL, srvThrottled.URL},
		alert.WithConcurrency(10),
	)
	require.NoError(t, err)
	ids := fc.EndpointIDs()
	require.Len(t, ids, 3)

	beforeOverflowA := metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(ids[0], alert.DropReasonOverflow))
	beforeOverflowB := metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(ids[1], alert.DropReasonOverflow))
	beforeOverflowC := metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(ids[2], alert.DropReasonOverflow))

	require.NoError(t, fc.Open(context.Background()))

	var producers sync.WaitGroup
	for rIdx := range numRules {
		producers.Add(1)
		go func(ruleNum int) {
			defer producers.Done()
			ns := "prod"
			name := fmt.Sprintf("burst-rule-%02d", ruleNum)
			evalID := fc.BeginEvaluation(alert.EvalMeta{
				Namespace:   ns,
				Name:        name,
				Destination: "icm://team",
				Region:      "eastus",
			})
			for row := range rowsPerRule {
				err := fc.EnqueueEvaluation(context.Background(), evalID, alert.Alert{
					Destination:   "icm://team",
					Title:         fmt.Sprintf("rule-%02d-row-%02d", ruleNum, row),
					Severity:      2,
					Source:        fmt.Sprintf("%s/%s", ns, name),
					CorrelationID: fmt.Sprintf("%s/%s://%d", ns, name, row),
				})
				require.NoError(t, err)
			}
			fc.SealEvaluation(evalID, false, nil, 0, false)
		}(rIdx)
	}
	producers.Wait()
	require.NoError(t, fc.Close())

	// Healthy endpoints receive ALL 250 alerts with 0 overflow drops and 0 notification-failure summaries
	require.Len(t, recA.regular, totalAlerts)
	require.Empty(t, recA.summaries)
	require.Len(t, recB.regular, totalAlerts)
	require.Empty(t, recB.summaries)

	require.Equal(t, beforeOverflowA, metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(ids[0], alert.DropReasonOverflow)))
	require.Equal(t, beforeOverflowB, metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(ids[1], alert.DropReasonOverflow)))
	require.Equal(t, beforeOverflowC, metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(ids[2], alert.DropReasonOverflow)))

	// Throttled endpoint delivered 1 regular alert per rule (10 total) and emitted exact-1 summary per rule (10 total)
	require.Len(t, recThrottled.regular, numRules)
	require.Len(t, recThrottled.summaries, numRules)

	seenSummarySources := make(map[string]int, numRules)
	for _, s := range recThrottled.summaries {
		seenSummarySources[s.Source]++
		require.Equal(t, s.Source, s.CorrelationID)
		require.Equal(t, 3, s.Severity)
		require.Contains(t, s.Summary, "Alerts suppressed by throttling:")
		// 24 of the 25 rows were suppressed on srvThrottled (15 shown in table + "9 additional suppressed alerts are not shown")
		require.Contains(t, s.Summary, "9 additional suppressed alerts are not shown")
	}
	require.Len(t, seenSummarySources, numRules)
	for src, count := range seenSummarySources {
		require.Equal(t, 1, count, "expected exact-1 summary POST for %s", src)
	}
}

func TestFanoutClient_ZeroRawWebhookURLsInMetricsLogsAndErrors(t *testing.T) {
	var logBuf syncBuffer
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prevLogger)

	var reqCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := reqCount.Add(1)
		switch n {
		case 1:
			w.WriteHeader(http.StatusBadRequest)
			// Echo complete request URI in error body
			_, _ = w.Write([]byte("upstream rejected " + r.URL.String()))
			return
		case 2:
			w.WriteHeader(http.StatusUnauthorized)
			// Echo individual query parameter value in a JSON field without the full query string
			_, _ = fmt.Fprintf(w, `{"error":"invalid_signature","sig":%q}`, r.URL.Query().Get("sig"))
			return
		case 3:
			// Echo secret URI and query token inside the HTTP/1.1 status reason phrase itself
			if hj, ok := w.(http.Hijacker); ok {
				if conn, buf, hjErr := hj.Hijack(); hjErr == nil {
					_, _ = fmt.Fprintf(buf, "HTTP/1.1 400 Rejected %s\r\nContent-Length: 0\r\nConnection: close\r\n\r\n", r.URL.String())
					_ = buf.Flush()
					_ = conn.Close()
					return
				}
			}
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	// Build secret-bearing URLs (credentials, secret path segment, secret query token)
	hostOnly := strings.TrimPrefix(srv.URL, "http://")
	secretGoodURL := fmt.Sprintf("http://hookuser:ULTRA_SECRET_PASS@%s/services/T999/B888/WEBHOOK_PATH_SECRET?sig=QUERY_SIG_SECRET", hostOnly)
	secretDeadURL := "http://deaduser:DEAD_SECRET_PASS@127.0.0.1:1/services/T777/B666/DEAD_PATH_SECRET?sig=DEAD_QUERY_SECRET"

	// Direct Client.Create network error + individual JSON-echoed query-param error checks
	directCli, err := alert.NewClient(200 * time.Millisecond)
	require.NoError(t, err)
	directErr := directCli.Create(context.Background(), secretDeadURL+"/alerts", alert.Alert{
		Title:    "direct-check",
		Severity: 2,
		Source:   "redact-ns/direct-rule",
	})
	require.Error(t, directErr)

	// Hit case 1 (echoed URI) and case 2 (echoed individual query sig) directly on Client.Create too
	epGood, err := alert.ParseBaseEndpoint(secretGoodURL)
	require.NoError(t, err)
	echoedURIErr := directCli.Create(context.Background(), epGood.URL, alert.Alert{Title: "e1", Severity: 2, Source: "redact-ns/r"})
	require.Error(t, echoedURIErr)
	echoedSigErr := directCli.Create(context.Background(), epGood.URL, alert.Alert{Title: "e2", Severity: 2, Source: "redact-ns/r"})
	require.Error(t, echoedSigErr)
	reasonPhraseErr := directCli.Create(context.Background(), epGood.URL, alert.Alert{Title: "e3", Severity: 2, Source: "redact-ns/r"})
	require.Error(t, reasonPhraseErr)
	reqCount.Store(0)

	fc, err := alert.NewFanoutClient(
		[]string{secretGoodURL, secretDeadURL},
		alert.WithConcurrency(1),
		alert.WithMaxQueueItems(4),
		alert.WithBackoff(5*time.Millisecond, 10*time.Millisecond, 1),
	)
	require.NoError(t, err)
	require.NoError(t, fc.Open(context.Background()))

	// Trigger permanent error (with echoed path), overflow, retry, and retry exhaustion
	for i := range 4 {
		require.NoError(t, fc.Create(context.Background(), "", alert.Alert{
			Title:    fmt.Sprintf("redact-alert-%d", i),
			Severity: 2,
			Source:   "redact-ns/redact-rule",
		}))
	}
	require.NoError(t, fc.Close())

	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	var metricsBuf bytes.Buffer
	for _, mf := range mfs {
		_, err := expfmt.MetricFamilyToText(&metricsBuf, mf)
		require.NoError(t, err)
	}

	forbiddenSubstrings := []string{
		secretGoodURL,
		secretDeadURL,
		"hookuser",
		"ULTRA_SECRET_PASS",
		"WEBHOOK_PATH_SECRET",
		"QUERY_SIG_SECRET",
		"/services/T999",
		"deaduser",
		"DEAD_SECRET_PASS",
		"DEAD_PATH_SECRET",
		"DEAD_QUERY_SECRET",
		"/services/T777",
	}

	for _, surface := range []struct {
		name string
		text string
	}{
		{name: "direct error", text: directErr.Error()},
		{name: "echoed URI error", text: echoedURIErr.Error()},
		{name: "echoed query sig error", text: echoedSigErr.Error()},
		{name: "reason phrase error", text: reasonPhraseErr.Error()},
		{name: "logs", text: logBuf.String()},
		{name: "/metrics", text: metricsBuf.String()},
	} {
		for _, secret := range forbiddenSubstrings {
			require.NotContains(t, surface.text, secret, "%s leaked %q", surface.name, secret)
		}
	}

	for _, id := range fc.EndpointIDs() {
		require.Contains(t, metricsBuf.String(), id)
		require.Contains(t, logBuf.String(), id)
	}
}

func TestClientAndFanout_Truncated400BodyStillFailsPermanentlyWithoutRetry(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		hj, ok := w.(http.Hijacker)
		require.True(t, ok)
		conn, buf, err := hj.Hijack()
		require.NoError(t, err)
		// Declare Content-Length: 500 on a 400 response, send 7 bytes, and close socket -> io.ErrUnexpectedEOF
		_, _ = buf.WriteString("HTTP/1.1 400 Bad Request\r\nContent-Length: 500\r\nConnection: close\r\n\r\npartial")
		_ = buf.Flush()
		_ = conn.Close()
	}))
	defer srv.Close()

	fc, err := alert.NewFanoutClient(
		[]string{srv.URL},
		alert.WithConcurrency(1),
		alert.WithBackoff(5*time.Millisecond, 10*time.Millisecond, 3),
	)
	require.NoError(t, err)
	epID := fc.EndpointIDs()[0]
	beforePerm := metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonPermanentError))

	require.NoError(t, fc.Open(context.Background()))
	require.NoError(t, fc.Create(context.Background(), "", alert.Alert{
		Title:    "truncated-400",
		Severity: 2,
		Source:   "ns/trunc-400",
	}))
	require.NoError(t, fc.Close())

	require.Equal(t, int32(1), hits.Load(), "truncated 400 response must not be retried")
	require.Equal(t, beforePerm+1, metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonPermanentError)))
}

func TestFanoutClient_Stalled400BodyTimeoutStillFailsPermanentlyWithoutRetry(t *testing.T) {
	var hits atomic.Int32
	stopHandler := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("head"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		// Stall body past the 50ms per-attempt timeout
		select {
		case <-r.Context().Done():
		case <-stopHandler:
		}
	}))
	defer srv.Close()
	defer close(stopHandler)

	fc, err := alert.NewFanoutClient(
		[]string{srv.URL},
		alert.WithConcurrency(1),
		alert.WithTimeout(50*time.Millisecond),
		alert.WithBackoff(5*time.Millisecond, 10*time.Millisecond, 3),
	)
	require.NoError(t, err)
	epID := fc.EndpointIDs()[0]
	beforePerm := metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonPermanentError))

	require.NoError(t, fc.Open(context.Background()))
	require.NoError(t, fc.Create(context.Background(), "", alert.Alert{
		Title:    "stalled-400",
		Severity: 2,
		Source:   "ns/stalled-400",
	}))

	require.NoError(t, fc.Close())
	require.Equal(t, int32(1), hits.Load(), "400 response whose body stalls until per-attempt timeout must not be retried")
	require.Equal(t, beforePerm+1, metricValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonPermanentError)))
}

func TestClient_QueryCredentialStraddling4KiBBoundaryLeavesZeroPrefixBytes(t *testing.T) {
	// 64-byte distinct query token starting at byte offset 4064 (straddling byte 4096)
	token64 := "QTOK" + strings.Repeat("7xK9mP2v", 7) + "_END"
	require.Len(t, token64, 64)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		prefix := strings.Repeat("A", 4064)
		echoed := r.URL.Query().Get("sig")
		suffix := strings.Repeat("Z", 128)
		_, _ = w.Write([]byte(prefix + echoed + suffix))
	}))
	defer srv.Close()

	ep, err := alert.ParseBaseEndpoint(srv.URL + "/hook?sig=" + token64)
	require.NoError(t, err)

	cli, err := alert.NewClient(5 * time.Second)
	require.NoError(t, err)
	gotErr := cli.Create(context.Background(), ep.URL, alert.Alert{
		Title:    "boundary-check",
		Severity: 2,
		Source:   "ns/boundary",
	})
	require.Error(t, gotErr)

	var statusErr *alert.StatusError
	require.ErrorAs(t, gotErr, &statusErr)
	require.Len(t, statusErr.Body, 4096)

	// Assert 0 prefix bytes of token64 appear in StatusError.Body or err.Error()
	for prefixLen := 4; prefixLen <= len(token64); prefixLen++ {
		sub := token64[:prefixLen]
		require.NotContains(t, statusErr.Body, sub, "StatusError.Body leaked %d-byte token prefix %q", prefixLen, sub)
		require.NotContains(t, gotErr.Error(), sub, "err.Error() leaked %d-byte token prefix %q", prefixLen, sub)
	}
	require.Contains(t, statusErr.Body[4060:], "<redacted>")
}

func TestSanitizeShortQueryParams_NoExponentialExpansionAndExactEndpointID(t *testing.T) {
	var logBuf syncBuffer
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prevLogger)

	rawBase := "https://hooks.example.com:8443/team,prod?env=a&mode=e&tier=d&sp=r&sig=1"
	ep, err := alert.ParseBaseEndpoint(rawBase)
	require.NoError(t, err)

	// Direct Client.Create against closed local port with the same query shape
	deadRaw := "http://127.0.0.1:1/team,prod?env=a&mode=e&tier=d&sp=r&sig=1"
	deadEP, err := alert.ParseBaseEndpoint(deadRaw)
	require.NoError(t, err)
	cli, err := alert.NewClient(200 * time.Millisecond)
	require.NoError(t, err)
	directErr := cli.Create(context.Background(), deadEP.URL, alert.Alert{
		Title:    "short-params",
		Severity: 2,
		Source:   "ns/short",
	})
	require.Error(t, directErr)
	require.Contains(t, directErr.Error(), deadEP.ID)
	require.NotContains(t, directErr.Error(), "team,prod")
	require.NotContains(t, directErr.Error(), "env=a")
	require.Less(t, len(directErr.Error()), 256)

	// FanoutClient returning *url.Error on https://hooks.example.com:8443/team,prod?env=a&mode=e&tier=d&sp=r&sig=1
	sender := senderFunc(func(ctx context.Context, endpoint string, _ alert.Alert) error {
		return &url.Error{
			Op:  "Post",
			URL: endpoint,
			Err: context.DeadlineExceeded,
		}
	})
	fc, err := alert.NewFanoutClient(
		[]string{rawBase},
		alert.WithSender(sender),
		alert.WithConcurrency(1),
		alert.WithBackoff(time.Millisecond, 2*time.Millisecond, 1),
	)
	require.NoError(t, err)
	require.NoError(t, fc.Open(context.Background()))
	require.NoError(t, fc.Create(context.Background(), "", alert.Alert{
		Title:    "fanout-short-params",
		Severity: 2,
		Source:   "ns/short",
	}))
	require.NoError(t, fc.Close())

	logs := logBuf.String()
	require.Contains(t, logs, ep.ID)
	require.NotContains(t, logs, "team,prod")
	require.NotContains(t, logs, "env=a")
	require.Less(t, len(logs), 2048)
}

func TestFanoutClient_SameHostPerEndpointTransportIsolationUnder100Concurrency(t *testing.T) {
	const workers = 100
	var slowInFlight atomic.Int32
	allSlowOccupied := make(chan struct{})
	releaseSlow := make(chan struct{})
	var fastDelivered atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/slow/") {
			if slowInFlight.Add(1) == workers {
				close(allSlowOccupied)
			}
			<-releaseSlow
			w.WriteHeader(http.StatusCreated)
			return
		}
		fastDelivered.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	fc, err := alert.NewFanoutClient(
		[]string{srv.URL + "/slow", srv.URL + "/fast"},
		alert.WithConcurrency(workers),
	)
	require.NoError(t, err)
	require.NoError(t, fc.Open(context.Background()))

	for i := range workers {
		require.NoError(t, fc.Create(context.Background(), "", alert.Alert{
			Title:    fmt.Sprintf("msg-%d", i),
			Severity: 2,
			Source:   "ns/same-host",
		}))
	}

	recvWithTimeout(t, allSlowOccupied, "100 slow workers occupied")

	// While all 100 connections on /slow are occupied against srv.URL's host, send one more alert:
	// /fast on the SAME host has its own isolated *http.Transport and delivers immediately.
	require.NoError(t, fc.Create(context.Background(), "", alert.Alert{
		Title:    "fast-probe",
		Severity: 2,
		Source:   "ns/same-host",
	}))
	require.Eventually(t, func() bool {
		return fastDelivered.Load() == workers+1
	}, 2*time.Second, 5*time.Millisecond)

	close(releaseSlow)
	require.NoError(t, fc.Close())
}
