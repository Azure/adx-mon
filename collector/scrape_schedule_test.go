package collector

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/remote"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
)

func TestScrapeSchedule_Reconcile(t *testing.T) {
	now := time.Now()
	target := ScrapeTarget{Addr: "http://localhost/metrics", ScrapeInterval: time.Second}
	key := target.key("pod", false)
	targets := map[scrapeTargetKey]ScrapeTarget{key: target}
	schedule := make(scrapeSchedule)
	schedule.reconcile(targets, time.Minute, now)
	first, ok := schedule.next()
	require.True(t, ok)
	require.False(t, first.Before(now))
	require.True(t, first.Before(now.Add(time.Second)))

	// Unrelated pod updates must not postpone a scheduled scrape.
	schedule.reconcile(targets, time.Minute, now.Add(100*time.Millisecond))
	unchanged, _ := schedule.next()
	require.Equal(t, first, unchanged)

	target.ScrapeInterval = 250 * time.Millisecond
	targets[key] = target
	schedule.reconcile(targets, time.Minute, now)
	require.Empty(t, schedule.due(now.Add(249*time.Millisecond)))
	require.Equal(t, []ScrapeTarget{target}, schedule.due(now.Add(250*time.Millisecond)))

	// Removing the override restores the global default for the next scrape.
	target.ScrapeInterval = 0
	targets[key] = target
	schedule.reconcile(targets, time.Minute, now)
	require.Empty(t, schedule.due(now.Add(time.Minute-time.Nanosecond)))
	require.Equal(t, []ScrapeTarget{target}, schedule.due(now.Add(time.Minute)))

	schedule.reconcile(nil, time.Minute, now)
	_, ok = schedule.next()
	require.False(t, ok)
	require.Empty(t, schedule.due(now.Add(time.Hour)))
}

func TestScrapeSchedule_SkipsMissedDeadlines(t *testing.T) {
	now := time.Now()
	target := ScrapeTarget{Addr: "http://localhost/metrics"}
	schedule := scrapeSchedule{target.key("", true): {
		target: target, interval: time.Second, next: now,
	}}
	require.Equal(t, []ScrapeTarget{target}, schedule.due(now))
	schedule.advance(now, now.Add(3500*time.Millisecond))
	require.Empty(t, schedule.due(now.Add(3500*time.Millisecond)))
	next, ok := schedule.next()
	require.True(t, ok)
	require.Equal(t, now.Add(4*time.Second), next)
}

func TestScraper_SchedulesMixedIntervals(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		intervals := map[string]time.Duration{
			"/fast":    100 * time.Millisecond,
			"/medium":  250 * time.Millisecond,
			"/default": time.Second,
			"/slow":    2 * time.Second,
		}
		var targets []ScrapeTarget
		for path, interval := range intervals {
			target := ScrapeTarget{Addr: "http://localhost" + path}
			if path != "/default" {
				target.ScrapeInterval = interval
			}
			targets = append(targets, target)
		}
		calls := make(map[string][]time.Time)
		startTestScraper(t, &ScraperOpts{
			ScrapeInterval: time.Second, Targets: targets,
		}, func(req *http.Request) (*http.Response, error) {
			calls[req.URL.Path] = append(calls[req.URL.Path], time.Now())
			return scrapeResponse(http.StatusOK), nil
		})

		time.Sleep(10 * time.Second)
		synctest.Wait()
		for path, interval := range intervals {
			want := int(10 * time.Second / interval)
			require.GreaterOrEqual(t, len(calls[path]), want, path)
			require.LessOrEqual(t, len(calls[path]), want+1, path)
			for i := 1; i < len(calls[path]); i++ {
				require.Equal(t, interval, calls[path][i].Sub(calls[path][i-1]), path)
			}
		}
	})
}

func TestScraper_HealthAndFailuresConsumeDeadlines(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		health := &scrapeTestHealth{}
		var calls []time.Time
		startTestScraper(t, &ScraperOpts{
			ScrapeInterval: time.Second, HealthChecker: health,
			Targets: []ScrapeTarget{{Addr: "http://localhost/metrics"}},
		}, func(req *http.Request) (*http.Response, error) {
			calls = append(calls, time.Now())
			return scrapeResponse(http.StatusInternalServerError), nil
		})
		time.Sleep(5 * time.Second)
		synctest.Wait()
		require.Empty(t, calls)

		health.healthy.Store(true)
		time.Sleep(5 * time.Second)
		synctest.Wait()
		require.Len(t, calls, 5)
		for i := 1; i < len(calls); i++ {
			require.Equal(t, time.Second, calls[i].Sub(calls[i-1]))
		}
	})
}

func TestScraper_SlowScrapesDoNotCatchUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls []time.Time
		startTestScraper(t, &ScraperOpts{
			ScrapeInterval: 100 * time.Millisecond,
			Targets:        []ScrapeTarget{{Addr: "http://localhost/metrics"}},
		}, func(req *http.Request) (*http.Response, error) {
			calls = append(calls, time.Now())
			time.Sleep(350 * time.Millisecond)
			return scrapeResponse(http.StatusOK), nil
		})
		time.Sleep(2 * time.Second)
		synctest.Wait()
		require.Len(t, calls, 5)
		for i := 1; i < len(calls); i++ {
			require.Equal(t, 400*time.Millisecond, calls[i].Sub(calls[i-1]))
		}
	})
}

func TestScraper_MaintainsConnectionsWithoutTargets(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := &scrapeTestRemote{}
		calls := 0
		startTestScraper(t, &ScraperOpts{
			ScrapeInterval: time.Second, RemoteClients: []remote.RemoteWriteClient{client},
		}, func(req *http.Request) (*http.Response, error) {
			calls++
			return scrapeResponse(http.StatusOK), nil
		})
		time.Sleep(10 * time.Minute)
		synctest.Wait()
		require.Equal(t, 2, client.closed)
		require.Zero(t, calls)
	})
}

func TestScraper_PodIntervalUpdates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex
		calls := make(map[string]int)
		count := func(path string) int {
			mu.Lock()
			defer mu.Unlock()
			return calls[path]
		}
		s := startTestScraper(t, &ScraperOpts{ScrapeInterval: time.Second}, func(req *http.Request) (*http.Response, error) {
			mu.Lock()
			calls[req.URL.Path]++
			mu.Unlock()
			return scrapeResponse(http.StatusOK), nil
		})
		p := fakePod("namespace", "pod", nil, "node")
		p.UID = "pod-uid"
		p.Annotations = map[string]string{
			"adx-mon/scrape": "true", "adx-mon/scrape-interval": "100ms",
			"adx-mon/targets": "/one:metrics,/two:9090",
		}
		p.Status.PodIP = "1.2.3.4"
		p.Spec.Containers = []v1.Container{{Name: "app", Ports: []v1.ContainerPort{
			{Name: "metrics", ContainerPort: 8080}, {ContainerPort: 9090},
		}}}
		s.OnAdd(p, true)
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		for _, path := range []string{"/one", "/two"} {
			require.GreaterOrEqual(t, count(path), 10)
			require.LessOrEqual(t, count(path), 11)
		}

		// Both endpoints must adopt an interval-only update immediately.
		updated := p.DeepCopy()
		updated.Annotations["adx-mon/scrape-interval"] = "200ms"
		s.OnUpdate(p, updated)
		synctest.Wait()
		one, two := count("/one"), count("/two")
		time.Sleep(time.Second)
		synctest.Wait()
		require.Equal(t, one+5, count("/one"))
		require.Equal(t, two+5, count("/two"))

		// Removing the annotation resets the next deadline to the default.
		p = updated.DeepCopy()
		delete(p.Annotations, "adx-mon/scrape-interval")
		s.OnUpdate(updated, p)
		synctest.Wait()
		one, two = count("/one"), count("/two")
		time.Sleep(time.Second - time.Nanosecond)
		synctest.Wait()
		require.Equal(t, one, count("/one"))
		require.Equal(t, two, count("/two"))
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		require.Equal(t, one+1, count("/one"))
		require.Equal(t, two+1, count("/two"))

		// An endpoint removed from the list must stop while the other continues.
		updated = p.DeepCopy()
		updated.Annotations["adx-mon/targets"] = "/one:metrics"
		s.OnUpdate(p, updated)
		synctest.Wait()
		one, two = count("/one"), count("/two")
		time.Sleep(2 * time.Second)
		synctest.Wait()
		require.Equal(t, one+2, count("/one"))
		require.Equal(t, two, count("/two"))

		p = updated.DeepCopy()
		p.Annotations["adx-mon/scrape"] = "false"
		s.OnUpdate(updated, p)
		synctest.Wait()
		one = count("/one")
		time.Sleep(2 * time.Second)
		synctest.Wait()
		require.Equal(t, one, count("/one"))

		// Re-enabling discovery must wake the idle scheduler; deleting the pod
		// must then discard its deadlines, including a long pending interval.
		p.Annotations["adx-mon/scrape"] = "true"
		s.OnAdd(p, false)
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		require.Greater(t, count("/one"), one)
		s.OnDelete(p)
		synctest.Wait()
		one = count("/one")
		time.Sleep(time.Hour)
		synctest.Wait()
		require.Equal(t, one, count("/one"))
	})
}

type scrapeTestRemote struct{ closed int }

func (c *scrapeTestRemote) Write(context.Context, *prompb.WriteRequest) error { return nil }
func (c *scrapeTestRemote) CloseIdleConnections()                             { c.closed++ }

type scrapeRoundTripper func(*http.Request) (*http.Response, error)

func (fn scrapeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type scrapeTestHealth struct{ healthy atomic.Bool }

func (h *scrapeTestHealth) IsHealthy() bool { return h.healthy.Load() }

// startTestScraper runs the real scheduling and scraping loop with in-memory
// HTTP responses so synctest controls all time without external network I/O.
func startTestScraper(t *testing.T, opts *ScraperOpts, fetch scrapeRoundTripper) *Scraper {
	t.Helper()
	opts.DisableDiscovery = true
	if opts.HealthChecker == nil {
		health := &scrapeTestHealth{}
		health.healthy.Store(true)
		opts.HealthChecker = health
	}
	s := NewScraper(opts)
	s.scrapeClient = &MetricsClient{
		client: &http.Client{Transport: fetch}, closing: make(chan struct{}),
	}
	for _, target := range opts.Targets {
		s.targets[target.key("", true)] = target
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.wg.Add(1)
	go s.scrape(ctx)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	synctest.Wait()
	return s
}

func scrapeResponse(status int) *http.Response {
	return &http.Response{
		StatusCode: status, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader("test_metric 1\n")),
	}
}
