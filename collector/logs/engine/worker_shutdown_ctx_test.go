package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/require"
)

// ctxBlockingSink is a sink that honors plain context cancellation but does NOT
// use the storage-wait mechanism (types.StorageWait). It blocks in Send until the
// batch context is done or the test explicitly releases it via `done`. This lets
// the test isolate the question "is the per-batch ctx cancelled when the worker
// shuts down?" independently of the durable storage-wait path.
type ctxBlockingSink struct {
	name    string
	done    chan struct{} // test closes this to unblock a Send that would otherwise
	mu      sync.Mutex
	entered chan struct{} // closed once Send is running
	closed  int           // number of times Send returned because ctx was cancelled
}

func (s *ctxBlockingSink) Open(context.Context) error { return nil }
func (s *ctxBlockingSink) Close() error               { return nil }
func (s *ctxBlockingSink) Name() string               { return s.name }

func (s *ctxBlockingSink) Send(ctx context.Context, batch *types.LogBatch) error {
	close(s.entered)

	select {
	case <-ctx.Done():
		// The batch context was cancelled; the sink honors it and returns promptly.
		s.mu.Lock()
		s.closed++
		s.mu.Unlock()
		return ctx.Err()
	case <-s.done:
		// The test released the sink so it can complete normally.
		batch.Ack()
		return nil
	}
}

func (s *ctxBlockingSink) NumClosed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// TestWorker_BatchContextCancelledOnShutdown verifies that the per-batch context
// derived inside RunWaitingForStorage is tied to the stop (shutdown) context, so
// that plain sinks which only honor ctx.Done() (i.e. NOT the storage-wait
// mechanism) are cancelled promptly when the component shuts down.
//
// Before the fix, the batch ctx was built from context.Background(), so a
// non-durable sink that honors only ctx.Done() would keep blocking forever after
// stop is cancelled, and Close() could stall.
func TestWorker_BatchContextCancelledOnShutdown(t *testing.T) {
	input := make(chan *types.LogBatch)
	sink := &ctxBlockingSink{
		name:    "ctx-blocking-sink",
		done:    make(chan struct{}),
		entered: make(chan struct{}),
	}

	w := &worker{
		SourceName:   "test-source",
		Input:        input,
		Sinks:        []types.Sink{sink},
		BatchTimeout: 30 * time.Second, // long enough that the batch ctx itself never times out
	}

	stop, cancelStop := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Go(func() {
		w.RunWaitingForStorage(stop)
	})

	batch := types.LogBatchPool.Get()
	log := types.LogPool.Get()
	log.SetBodyValue("test", "value")
	batch.Logs = append(batch.Logs, log)

	input <- batch

	// Wait until the sink has entered Send and is blocked on the batch ctx.
	<-sink.entered

	// Shut down while the sink is in-flight. Before the fix, the batch ctx was
	// context.Background()-derived and the storage-wait value (the only link to
	// stop) is never consulted by this plain sink, so it stays blocked forever.
	cancelStop()

	// The sink must observe the cancellation and return promptly. Give a few
	// seconds of slack; a correct implementation unblocks immediately.
	select {
	case <-sink.done:
		t.Fatal("sink completed normally instead of via ctx cancellation")
	default:
	}
	require.Eventually(t, func() bool { return sink.NumClosed() > 0 }, 5*time.Second, 10*time.Millisecond,
		"sink was not cancelled on shutdown: the per-batch context did not derive from the stop context")
}
