package queue

// TODO: Make this configurable.
var concurrency = 5

// TODO: Keep track of failures for each execution so we can modify the lookback for subsequent queries.

// TODO: This is a very basic work queue to ensure we don't get throttled. We'll add features as needed.

// TODO: Add timeouts.

// Workers is a very lame work queue.
var Workers = make(chan struct{}, concurrency)
