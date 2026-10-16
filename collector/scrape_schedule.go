package collector

import (
	"math/rand"
	"sort"
	"time"
)

// scrapeSchedule is owned by the scraper goroutine. Informer callbacks only
// update the desired target set and wake that goroutine to reconcile it.
type scrapeSchedule map[scrapeTargetKey]*scheduledScrape

type scheduledScrape struct {
	target   ScrapeTarget
	interval time.Duration
	next     time.Time
}

func (s scrapeSchedule) reconcile(targets map[scrapeTargetKey]ScrapeTarget, defaultInterval time.Duration, now time.Time) {
	for key := range s {
		if _, ok := targets[key]; !ok {
			delete(s, key)
		}
	}
	for key, target := range targets {
		if existing, ok := s[key]; ok && existing.target.Equals(target) {
			continue
		}
		interval := target.ScrapeInterval
		if interval <= 0 {
			interval = defaultInterval
		}
		delay := interval
		if _, exists := s[key]; !exists {
			// Spread new targets over their own interval, including subsecond
			// intervals, rather than delaying them by the global interval.
			delay = time.Duration(rand.Int63n(int64(interval)))
		}
		s[key] = &scheduledScrape{target: target, interval: interval, next: now.Add(delay)}
	}
}

func (s scrapeSchedule) due(now time.Time) []ScrapeTarget {
	var targets []ScrapeTarget
	for _, entry := range s {
		if !entry.next.After(now) {
			targets = append(targets, entry.target)
		}
	}
	// Retain stable scrape ordering when multiple targets are due together.
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].path() == targets[j].path() {
			return targets[i].Addr < targets[j].Addr
		}
		return targets[i].path() < targets[j].path()
	})
	return targets
}

// advance consumes every deadline in the batch, even after a failed or skipped
// scrape. Missed intervals are skipped, preventing bursts after slow scrapes or
// recovery from an unhealthy collector.
func (s scrapeSchedule) advance(batchTime, finished time.Time) {
	for _, entry := range s {
		if !entry.next.After(batchTime) {
			entry.next = finished.Add(entry.interval - finished.Sub(entry.next)%entry.interval)
		}
	}
}

func (s scrapeSchedule) next() (time.Time, bool) {
	var next time.Time
	for _, entry := range s {
		if next.IsZero() || entry.next.Before(next) {
			next = entry.next
		}
	}
	return next, !next.IsZero()
}
