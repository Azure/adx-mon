package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/metrics"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

type mockTransformer struct {
	name      string
	transform func(context.Context, *types.LogBatch) (*types.LogBatch, error)
}

func (t *mockTransformer) Open(context.Context) error { return nil }
func (t *mockTransformer) Close() error               { return nil }
func (t *mockTransformer) Name() string               { return t.name }
func (t *mockTransformer) Transform(ctx context.Context, batch *types.LogBatch) (*types.LogBatch, error) {
	return t.transform(ctx, batch)
}

type mockSink struct {
	name    string
	sends   []*types.LogBatch
	sendErr error
	mu      sync.Mutex
}

func (s *mockSink) Open(context.Context) error { return nil }
func (s *mockSink) Close() error               { return nil }
func (s *mockSink) Name() string               { return s.name }
func (s *mockSink) Send(ctx context.Context, batch *types.LogBatch) error {
	if s.sendErr != nil {
		return s.sendErr
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.mu.Lock()
	s.sends = append(s.sends, batch)
	s.mu.Unlock()
	batch.Ack()
	return nil
}

func (s *mockSink) NumSends() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sends)
}

func TestWorker_BasicProcessing(t *testing.T) {
	input := make(chan *types.LogBatch)
	sink := &mockSink{name: "test-sink"}
	w := &worker{
		SourceName:   "test-source",
		Input:        input,
		Sinks:        []types.Sink{sink},
		BatchTimeout: time.Second,
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.Run()
	}()

	// Send a test batch
	batch := types.LogBatchPool.Get(1).(*types.LogBatch)
	batch.Reset()
	log := types.LogPool.Get(1).(*types.Log)
	log.Reset()
	log.SetBodyValue("test", "value")
	batch.Logs = append(batch.Logs, log)

	input <- batch
	close(input) // Signal worker to stop

	wg.Wait()

	// Verify batch was processed
	require.Equal(t, 1, sink.NumSends())

	// Verify metrics
	var m dto.Metric
	require.NoError(t, metrics.LogsCollectorLogsSent.WithLabelValues("test-source", "test-sink").Write(&m))
	require.Equal(t, float64(1), *m.Counter.Value)
}

func TestWorker_TransformError(t *testing.T) {
	input := make(chan *types.LogBatch)
	failingTransformer := &mockTransformer{
		name: "failing-transformer",
		transform: func(ctx context.Context, batch *types.LogBatch) (*types.LogBatch, error) {
			return batch, errors.New("transform error")
		},
	}
	sink := &mockSink{name: "test-sink"}

	w := &worker{
		SourceName:   "test-source",
		Input:        input,
		Transforms:   []types.Transformer{failingTransformer},
		Sinks:        []types.Sink{sink},
		BatchTimeout: time.Second,
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.Run()
	}()

	batch := types.LogBatchPool.Get(1).(*types.LogBatch)
	batch.Reset()
	log := types.LogPool.Get(1).(*types.Log)
	log.Reset()
	log.SetBodyValue("test", "value")
	batch.Logs = append(batch.Logs, log)

	input <- batch
	close(input)
	wg.Wait() // Wait for worker to finish

	// Verify no logs were sent and metrics were updated
	require.Equal(t, 0, sink.NumSends())

	var m dto.Metric
	require.NoError(t, metrics.LogsCollectorLogsDropped.WithLabelValues("test-source", "failing-transformer").Write(&m))
	require.Equal(t, float64(1), *m.Counter.Value)
}

func TestWorker_SinkError(t *testing.T) {
	input := make(chan *types.LogBatch)
	sink := &mockSink{
		name:    "failing-sink",
		sendErr: errors.New("sink error"),
	}

	w := &worker{
		SourceName:   "test-source",
		Input:        input,
		Sinks:        []types.Sink{sink},
		BatchTimeout: time.Second,
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.Run()
	}()

	batch := types.LogBatchPool.Get(1).(*types.LogBatch)
	batch.Reset()
	log := types.LogPool.Get(1).(*types.Log)
	log.Reset()
	log.SetBodyValue("test", "value")
	batch.Logs = append(batch.Logs, log)

	input <- batch
	close(input)

	wg.Wait()

	// Verify metrics were updated
	var m dto.Metric
	require.NoError(t, metrics.LogsCollectorLogsDropped.WithLabelValues("test-source", "failing-sink").Write(&m))
	require.Equal(t, float64(1), *m.Counter.Value)
}

func TestWorker_MultipleSinksConcurrent(t *testing.T) {
	t.Run("all sinks succeed", func(t *testing.T) {
		metrics.LogsCollectorLogsDropped.Reset()
		metrics.LogsCollectorLogsSent.Reset()

		input := make(chan *types.LogBatch)
		sink1 := &mockSink{name: "sink1"}
		sink2 := &mockSink{name: "sink2"}

		w := &worker{
			SourceName:   "test-source",
			Input:        input,
			Sinks:        []types.Sink{sink1, sink2},
			BatchTimeout: time.Second,
		}

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.Run()
		}()

		batch := types.LogBatchPool.Get(1).(*types.LogBatch)
		batch.Reset()
		log := types.LogPool.Get(1).(*types.Log)
		log.Reset()
		log.SetBodyValue("test", "value")
		batch.Logs = append(batch.Logs, log)

		input <- batch
		close(input)

		wg.Wait()

		require.Equal(t, 1, sink1.NumSends())
		require.Equal(t, 1, sink2.NumSends())

		// Verify metrics for both sinks
		var m1, m2 dto.Metric
		require.NoError(t, metrics.LogsCollectorLogsSent.WithLabelValues("test-source", "sink1").Write(&m1))
		require.NoError(t, metrics.LogsCollectorLogsSent.WithLabelValues("test-source", "sink2").Write(&m2))
		require.Equal(t, float64(1), *m1.Counter.Value)
		require.Equal(t, float64(1), *m2.Counter.Value)
	})

	t.Run("one sink fails", func(t *testing.T) {
		metrics.LogsCollectorLogsDropped.Reset()
		metrics.LogsCollectorLogsSent.Reset()

		input := make(chan *types.LogBatch)
		sink1 := &mockSink{name: "sink1", sendErr: errors.New("sink1 error")}
		sink2 := &mockSink{name: "sink2"}

		w := &worker{
			SourceName:   "test-source",
			Input:        input,
			Sinks:        []types.Sink{sink1, sink2},
			BatchTimeout: time.Second,
		}

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.Run()
		}()

		batch := types.LogBatchPool.Get(1).(*types.LogBatch)
		batch.Reset()
		log := types.LogPool.Get(1).(*types.Log)
		log.Reset()
		log.SetBodyValue("test", "value")
		batch.Logs = append(batch.Logs, log)

		input <- batch
		close(input)

		wg.Wait()

		// Verify only sink2 received the batch
		require.Equal(t, 0, sink1.NumSends())
		require.Equal(t, 1, sink2.NumSends())

		// Verify metrics show dropped logs for sink1 and successful send for sink2
		var dropped, sent dto.Metric
		require.NoError(t, metrics.LogsCollectorLogsDropped.WithLabelValues("test-source", "sink1").Write(&dropped))
		require.NoError(t, metrics.LogsCollectorLogsSent.WithLabelValues("test-source", "sink2").Write(&sent))
		require.Equal(t, float64(1), *dropped.Counter.Value)
		require.Equal(t, float64(1), *sent.Counter.Value)
	})
}

func TestWorker_BatchTimeout(t *testing.T) {
	input := make(chan *types.LogBatch)
	sink := &mockSink{name: "test-sink"}
	w := &worker{
		SourceName:   "test-source",
		Input:        input,
		Sinks:        []types.Sink{sink},
		BatchTimeout: 10 * time.Millisecond,
	}

	// Simulate already timed out
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
	cancel() // immediately cancelled

	batch := types.LogBatchPool.Get(1).(*types.LogBatch)
	batch.Reset()
	log := types.LogPool.Get(1).(*types.Log)
	log.Reset()
	log.SetBodyValue("test", "value")
	batch.Logs = append(batch.Logs, log)

	w.processBatch(ctx, batch)

	var m dto.Metric
	require.NoError(t, metrics.LogsCollectorLogsDropped.WithLabelValues("test-source", "test-sink").Write(&m))
	require.Equal(t, float64(1), *m.Counter.Value)
}
