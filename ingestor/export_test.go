package ingestor

import "time"

// stallTimeoutForTest overrides the drain stall budget and returns the previous
// value so tests can restore it.
func stallTimeoutForTest(d time.Duration) time.Duration {
	previous := stallTimeout
	stallTimeout = d
	return previous
}
