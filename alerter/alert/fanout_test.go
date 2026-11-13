package alert_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeEndpoint(t *testing.T) {
	require.Equal(t, "", alert.NormalizeEndpoint(""))
	require.Equal(t, "", alert.NormalizeEndpoint("   "))
	require.Equal(t, "http://notifier:8080/alerts", alert.NormalizeEndpoint("http://notifier:8080"))
	require.Equal(t, "http://notifier:8080/alerts", alert.NormalizeEndpoint("http://notifier:8080/"))
	require.Equal(t, "http://notifier:8080/alerts", alert.NormalizeEndpoint("http://notifier:8080/alerts"))
	require.Equal(t, "http://notifier:8080/alerts", alert.NormalizeEndpoint("http://notifier:8080/alerts/"))
}

func TestNewFanoutClient_DeduplicatesAndValidatesEndpoints(t *testing.T) {
	_, err := alert.NewFanoutClient(nil)
	require.Error(t, err)

	_, err = alert.NewFanoutClient([]string{"", "   "})
	require.Error(t, err)

	cli, err := alert.NewFanoutClient([]string{
		"http://notifier-a:8080",
		"http://notifier-a:8080/",
		"http://notifier-a:8080/alerts",
		"http://notifier-b:8080",
	})
	require.NoError(t, err)
	require.Equal(t, []string{
		"http://notifier-a:8080/alerts",
		"http://notifier-b:8080/alerts",
	}, cli.Endpoints())
}

func TestFanoutClient_1ToNFanoutAndOrdering(t *testing.T) {
	var muA, muB sync.Mutex
	var receivedA, receivedB []alert.Alert

	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/alerts", r.URL.Path)
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var a alert.Alert
		require.NoError(t, json.Unmarshal(b, &a))
		muA.Lock()
		receivedA = append(receivedA, a)
		muA.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	defer serverA.Close()

	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/alerts", r.URL.Path)
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var a alert.Alert
		require.NoError(t, json.Unmarshal(b, &a))
		muB.Lock()
		receivedB = append(receivedB, a)
		muB.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	defer serverB.Close()

	cli, err := alert.NewFanoutClient([]string{serverA.URL, serverB.URL}, alert.WithTimeout(time.Second))
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, cli.Open(ctx))
	defer func() { require.NoError(t, cli.Close()) }()

	titles := []string{"alert-1", "alert-2", "alert-3", "alert-4"}
	for i, title := range titles {
		custom := map[string]string{"seq": title}
		require.NoError(t, cli.Create(ctx, "", alert.Alert{
			Title:        title,
			Severity:     i + 1,
			Source:       "ns/rule",
			CustomFields: custom,
		}))
		// Mutate caller's map after Create to verify deep copy isolation.
		custom["seq"] = "mutated"
	}

	require.Eventually(t, func() bool {
		muA.Lock()
		defer muA.Unlock()
		muB.Lock()
		defer muB.Unlock()
		return len(receivedA) == len(titles) && len(receivedB) == len(titles)
	}, 2*time.Second, 10*time.Millisecond)

	muA.Lock()
	defer muA.Unlock()
	muB.Lock()
	defer muB.Unlock()

	for i, title := range titles {
		require.Equal(t, title, receivedA[i].Title)
		require.Equal(t, title, receivedA[i].CustomFields["seq"])
		require.Equal(t, title, receivedB[i].Title)
		require.Equal(t, title, receivedB[i].CustomFields["seq"])
	}
}

func TestFanoutClient_PerChannelBackoffRetryPreservesOrderAndIsolatesHealthyChannel(t *testing.T) {
	var muSlow, muFast sync.Mutex
	var slowDelivered, fastDelivered []string
	var slowAttemptsForFirst atomic.Int32

	sender := &funcSender{
		fn: func(_ context.Context, endpoint string, a alert.Alert) error {
			if endpoint == "http://fast/alerts" {
				muFast.Lock()
				fastDelivered = append(fastDelivered, a.Title)
				muFast.Unlock()
				return nil
			}
			// Slow endpoint fails the first 2 attempts of alert-1, then succeeds.
			if a.Title == "alert-1" && slowAttemptsForFirst.Add(1) <= 2 {
				return errors.New("temporary upstream failure")
			}
			muSlow.Lock()
			slowDelivered = append(slowDelivered, a.Title)
			muSlow.Unlock()
			return nil
		},
	}

	cli, err := alert.NewFanoutClient(
		[]string{"http://slow", "http://fast"},
		alert.WithSender(sender),
		alert.WithBackoff(15*time.Millisecond, 100*time.Millisecond, 5),
	)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, cli.Open(ctx))
	defer func() { require.NoError(t, cli.Close()) }()

	for _, title := range []string{"alert-1", "alert-2", "alert-3"} {
		require.NoError(t, cli.Create(ctx, "", alert.Alert{Title: title, Source: "ns/rule"}))
	}

	// Fast channel delivers all 3 even while slow channel is backing off.
	require.Eventually(t, func() bool {
		muFast.Lock()
		defer muFast.Unlock()
		return len(fastDelivered) == 3
	}, time.Second, 5*time.Millisecond)

	// Slow channel retries alert-1 and delivers alert-1, alert-2, alert-3 in order.
	require.Eventually(t, func() bool {
		muSlow.Lock()
		defer muSlow.Unlock()
		return len(slowDelivered) == 3
	}, 2*time.Second, 10*time.Millisecond)

	require.Equal(t, int32(3), slowAttemptsForFirst.Load())
	muSlow.Lock()
	require.Equal(t, []string{"alert-1", "alert-2", "alert-3"}, slowDelivered)
	muSlow.Unlock()
	muFast.Lock()
	require.Equal(t, []string{"alert-1", "alert-2", "alert-3"}, fastDelivered)
	muFast.Unlock()
}

func TestFanoutClient_DropOldestWhenMoreThanFiveQueuedAndTryNextImmediately(t *testing.T) {
	firstAttemptFailed := make(chan struct{})
	var failFirstOnce sync.Once
	var allowDelivery atomic.Bool

	var mu sync.Mutex
	var delivered []string

	sender := &funcSender{
		fn: func(_ context.Context, _ string, a alert.Alert) error {
			if !allowDelivery.Load() {
				if a.Title == "msg-1" {
					failFirstOnce.Do(func() { close(firstAttemptFailed) })
				}
				return errors.New("endpoint unavailable")
			}
			mu.Lock()
			delivered = append(delivered, a.Title)
			mu.Unlock()
			return nil
		},
	}

	// Use a long backoff (10s) so that if msg-1 were NOT evicted and cancelled when msg-6
	// arrives, the test would time out waiting for msg-2..msg-6 to be delivered.
	cli, err := alert.NewFanoutClient(
		[]string{"http://endpoint-a"},
		alert.WithSender(sender),
		alert.WithMaxQueueSize(5),
		alert.WithBackoff(10*time.Second, 30*time.Second, 5),
	)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, cli.Open(ctx))
	defer func() { require.NoError(t, cli.Close()) }()

	// Enqueue msg-1 and wait for its first attempt to fail so it is sitting in the 10s retry backoff.
	require.NoError(t, cli.Create(ctx, "", alert.Alert{Title: "msg-1", Source: "ns/rule"}))
	select {
	case <-firstAttemptFailed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for msg-1 initial failure")
	}

	// Enqueue msg-2, msg-3, msg-4, msg-5 (queue now has 5 messages: msg-1..msg-5).
	for _, title := range []string{"msg-2", "msg-3", "msg-4", "msg-5"} {
		require.NoError(t, cli.Create(ctx, "", alert.Alert{Title: title, Source: "ns/rule"}))
	}

	// Flip allowDelivery to true and enqueue msg-6 (> 5 messages in the queue).
	// Enqueuing msg-6 must drop msg-1 (the oldest message), cancel msg-1's 10s backoff sleep,
	// and immediately try msg-2, followed by msg-3, msg-4, msg-5, msg-6 in order.
	allowDelivery.Store(true)
	require.NoError(t, cli.Create(ctx, "", alert.Alert{Title: "msg-6", Source: "ns/rule"}))

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(delivered) == 5
	}, time.Second, 10*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"msg-2", "msg-3", "msg-4", "msg-5", "msg-6"}, delivered)
}

func TestFanoutClient_RetriesExhaustedMovesToNextMessage(t *testing.T) {
	var mu sync.Mutex
	var delivered []string
	var msg1Attempts atomic.Int32

	sender := &funcSender{
		fn: func(_ context.Context, _ string, a alert.Alert) error {
			if a.Title == "msg-1" {
				msg1Attempts.Add(1)
				return errors.New("persistent failure for msg-1")
			}
			mu.Lock()
			delivered = append(delivered, a.Title)
			mu.Unlock()
			return nil
		},
	}

	cli, err := alert.NewFanoutClient(
		[]string{"http://endpoint-a"},
		alert.WithSender(sender),
		alert.WithBackoff(5*time.Millisecond, 10*time.Millisecond, 2),
	)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, cli.Open(ctx))
	defer func() { require.NoError(t, cli.Close()) }()

	require.NoError(t, cli.Create(ctx, "", alert.Alert{Title: "msg-1", Source: "ns/rule"}))
	require.NoError(t, cli.Create(ctx, "", alert.Alert{Title: "msg-2", Source: "ns/rule"}))

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(delivered) == 1
	}, time.Second, 5*time.Millisecond)

	// Initial attempt + 2 retries = 3 attempts for msg-1 before moving to msg-2.
	require.Equal(t, int32(3), msg1Attempts.Load())
	mu.Lock()
	require.Equal(t, []string{"msg-2"}, delivered)
	mu.Unlock()
}

type funcSender struct {
	fn func(ctx context.Context, endpoint string, a alert.Alert) error
}

func (s *funcSender) Create(ctx context.Context, endpoint string, a alert.Alert) error {
	return s.fn(ctx, endpoint, a)
}
