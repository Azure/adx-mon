package collector

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/remote"
	"github.com/stretchr/testify/require"
)

// sampleRecorder records the timestamps of the samples written for each pod.
type sampleRecorder struct {
	mu         sync.Mutex
	timestamps map[string][]int64
}

func (r *sampleRecorder) Write(_ context.Context, wr *prompb.WriteRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ts := range wr.Timeseries {
		for l := range wr.Labels(ts) {
			if string(l.Name) == "adxmon_pod" {
				for _, s := range ts.Samples {
					r.timestamps[string(l.Value)] = append(r.timestamps[string(l.Value)], s.Timestamp)
				}
			}
		}
	}
	return nil
}

func (r *sampleRecorder) CloseIdleConnections() {}

// A slow target must not delay scrapes of other targets, and each target's samples are stamped with the time that
// target was fetched rather than when its batch of due targets started.
func TestScraper_SlowTargetDoesNotDelayOthers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		targets := []ScrapeTarget{{Addr: "http://localhost/busy", Pod: "busy"}}
		for _, pod := range []string{"ok1", "ok2", "ok3", "ok4", "ok5"} {
			targets = append(targets, ScrapeTarget{Addr: "http://localhost/" + pod, Pod: pod})
		}

		var mu sync.Mutex
		fetches := make(map[string][]int64)
		recorder := &sampleRecorder{timestamps: make(map[string][]int64)}
		startTestScraper(t, &ScraperOpts{
			ScrapeInterval: time.Second,
			ScrapeTimeout:  10 * time.Second,
			Targets:        targets,
			MaxBatchSize:   1000,
			RemoteClients:  []remote.RemoteWriteClient{recorder},
		}, func(req *http.Request) (*http.Response, error) {
			pod := req.URL.Path[1:]
			mu.Lock()
			fetches[pod] = append(fetches[pod], time.Now().UnixNano()/1e6)
			mu.Unlock()
			if pod == "busy" {
				// Responds just before the scrape timeout.
				time.Sleep(5 * time.Second)
			} else {
				time.Sleep(200 * time.Millisecond)
			}
			return scrapeResponse(http.StatusOK), nil
		})
		time.Sleep(10 * time.Second)
		synctest.Wait()

		mu.Lock()
		defer mu.Unlock()
		recorder.mu.Lock()
		defer recorder.mu.Unlock()
		for _, pod := range []string{"ok1", "ok2", "ok3", "ok4", "ok5"} {
			// skew is the largest distance between a sample's timestamp and the closest fetch of its target.
			var skew int64
			for _, ts := range recorder.timestamps[pod] {
				closest := int64(-1)
				for _, f := range fetches[pod] {
					if d := max(f-ts, ts-f); closest < 0 || d < closest {
						closest = d
					}
				}
				skew = max(skew, closest)
			}
			t.Logf("%s: %d scrapes in 10s at a 1s interval, samples stamped up to %dms from their fetch", pod, len(fetches[pod]), skew)
			require.GreaterOrEqual(t, len(fetches[pod]), 9, "%s was delayed by the slow target", pod)
			require.NotEmpty(t, recorder.timestamps[pod])
			require.Zero(t, skew, "%s samples were not stamped with their fetch time", pod)
		}
		t.Logf("busy: %d scrapes", len(fetches["busy"]))
	})
}

// A target that never responds is abandoned at its scrape interval when that is shorter than the scrape timeout.
func TestScraper_TimeoutCappedAtInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex
		var waited []time.Duration
		startTestScraper(t, &ScraperOpts{
			ScrapeInterval: time.Second,
			ScrapeTimeout:  10 * time.Second,
			Targets:        []ScrapeTarget{{Addr: "http://localhost/metrics"}},
		}, func(req *http.Request) (*http.Response, error) {
			start := time.Now()
			select {
			case <-req.Context().Done():
			case <-time.After(10 * time.Second):
			}
			mu.Lock()
			waited = append(waited, time.Since(start))
			mu.Unlock()
			return nil, context.DeadlineExceeded
		})
		time.Sleep(25 * time.Second)
		synctest.Wait()

		mu.Lock()
		defer mu.Unlock()
		require.NotEmpty(t, waited)
		t.Logf("a hung scrape held its target for %s", waited[0])
		require.Equal(t, time.Second, waited[0])
	})
}

// No more than ScrapeConcurrency scrapes run at once.
func TestScraper_ConcurrencyIsBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var targets []ScrapeTarget
		for _, pod := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
			targets = append(targets, ScrapeTarget{Addr: "http://localhost/" + pod, Pod: pod})
		}
		var running, peak atomic.Int64
		startTestScraper(t, &ScraperOpts{
			ScrapeInterval:    time.Second,
			ScrapeConcurrency: 4,
			Targets:           targets,
		}, func(req *http.Request) (*http.Response, error) {
			n := running.Add(1)
			for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
			}
			time.Sleep(900 * time.Millisecond)
			running.Add(-1)
			return scrapeResponse(http.StatusOK), nil
		})
		time.Sleep(5 * time.Second)
		synctest.Wait()
		require.EqualValues(t, 4, peak.Load())
	})
}
