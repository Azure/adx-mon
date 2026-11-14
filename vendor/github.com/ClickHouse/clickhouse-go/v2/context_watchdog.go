package clickhouse

import (
	"context"
	"sync"
)

// contextWatchdog is a helper function to run a callback when the context is done.
// Its idempotent stop function prevents the callback from running, or waits for
// an already-running callback to finish. Stop before releasing a resource that
// the callback can access, so cancellation cannot affect its next owner.
// Useful for interrupting some logic when the context is done,
// but you want to not bother about context cancellation if your logic is already done.
// Example:
// stopCW := contextWatchdog(ctx, func() { /* do something */ })
// // do something else
// defer stopCW()
func contextWatchdog(ctx context.Context, callback func()) (cancel func()) {
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(done)
		callback()
	})
	return sync.OnceFunc(func() {
		if !stop() {
			<-done
		}
	})
}
