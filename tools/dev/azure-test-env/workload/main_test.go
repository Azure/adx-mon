package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestScrapeStopsRepeatingExpiredSample(t *testing.T) {
	mu.Lock()
	latest = signal{RunID: "run", Seq: 1, EmittedMS: time.Now().UnixMilli()}
	mu.Unlock()

	fresh := httptest.NewRecorder()
	scrape(fresh, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(fresh.Body.String(), `run_id="run"`) {
		t.Fatalf("new sample not exposed: %q", fresh.Body.String())
	}

	mu.Lock()
	latest.EmittedMS = time.Now().Add(-3 * time.Second).UnixMilli()
	mu.Unlock()
	expired := httptest.NewRecorder()
	scrape(expired, httptest.NewRequest("GET", "/metrics", nil))
	if strings.Contains(expired.Body.String(), "e2e_scraped_sample") {
		t.Fatalf("expired sample is still exposed: %q", expired.Body.String())
	}
}
