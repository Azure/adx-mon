package queue

const DefaultConcurrency = 5

// TODO: Keep track of failures for each execution so we can modify the lookback for subsequent queries.

// TODO: This is a very basic work queue to ensure we don't get throttled. We'll add features as needed.

// TODO: Add timeouts.

// New returns a basic worker queue with the requested concurrency. Invalid or
// unspecified values fall back to the historical default.
func New(concurrency int) chan struct{} {
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}

	return make(chan struct{}, concurrency)
}
