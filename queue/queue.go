package queue

import "time"

// TODO: Make this configurable.
var concurrency = 5

// TODO: Keep track of failures for each execution so we can modify the lookback for subsequent queries.

// TODO: This is a very basic work queue to ensure we don't get throttled. We'll add features as needed.

// TODO: Add timeouts.

// Workers is a very lame work queue.
var Workers = make(chan struct{}, concurrency)

// Result contains the result of a query.
type Result struct {
	// Error is the error returned by the query, if there was one.
	Error error

	// Timestamp is the time the query was run.
	Timestamp time.Time
}
