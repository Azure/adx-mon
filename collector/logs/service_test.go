package logs_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector/logs"
	"github.com/Azure/adx-mon/collector/logs/engine"
	"github.com/Azure/adx-mon/collector/logs/sinks"
	"github.com/Azure/adx-mon/collector/logs/sources"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/metrics"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

const sourceName = "ConstSource"

func BenchmarkPipeline(b *testing.B) {
	for i := 0; i < b.N; i++ {
		sink := sinks.NewCountingSink(10000)
		source := sources.NewConstSource("test-val", 1*time.Second, 1000, engine.WorkerCreator(nil, sink))

		service := &logs.Service{
			Source: source,
			Sink:   sink,
		}
		context := context.Background()

		service.Open(context)
		<-sink.DoneChan()
		service.Close()
	}
}

func TestPipeline(t *testing.T) {
	// Ensure we can send 10k logs through the pipeline.
	numLogs := int64(10000)
	sink := sinks.NewCountingSink(numLogs)
	source := sources.NewConstSource("test-val", 1*time.Second, 1000, engine.WorkerCreator(nil, sink))

	service := &logs.Service{
		Source: source,
		Sink:   sink,
	}
	context := context.Background()

	service.Open(context)
	<-sink.DoneChan()
	service.Close()

	countOfDropped := getCounterValue(t, metrics.LogsCollectorLogsDropped.WithLabelValues(sourceName, sink.Name()))
	require.Equal(t, countOfDropped, float64(0))
	countOfSent := getCounterValue(t, metrics.LogsCollectorLogsSent.WithLabelValues(sourceName, sink.Name()))
	require.GreaterOrEqual(t, countOfSent, float64(numLogs)) // batching sends more logs which is ok
}

func TestLifecycle(t *testing.T) {
	// Ensure we can open and close the service cleanly
	// source only sends the log when it is closed, simulating a flush on close.
	// transformer panics if told to transform when it is closed.
	// sink panics if it receives a log after it is closed.
	sink := sinks.NewCountingSink(1)
	transform := &transformerpanicifclosed{}
	transforms := []types.Transformer{transform}
	source := newSourceSendOnClose(engine.WorkerCreator(transforms, sink))

	service := &logs.Service{
		Source:     source,
		Transforms: transforms,
		Sink:       sink,
	}
	context := context.Background()

	err := service.Open(context)
	if err != nil {
		t.Fatalf("Failed to open service: %v", err)
	}
	service.Close()
	<-sink.DoneChan()

	countOfDropped := getCounterValue(t, metrics.LogsCollectorLogsDropped.WithLabelValues(sourceName, sink.Name()))
	require.Equal(t, countOfDropped, float64(0))
	countOfSent := getCounterValue(t, metrics.LogsCollectorLogsSent.WithLabelValues(sourceName, sink.Name()))
	require.GreaterOrEqual(t, countOfSent, float64(1)) // batching sends more than 1
}

func getCounterValue(t *testing.T, metric prometheus.Metric) float64 {
	t.Helper()

	metricDTO := &dto.Metric{}
	err := metric.Write(metricDTO)
	require.NoError(t, err)
	return metricDTO.Counter.GetValue()
}

type sourceSendOnClose struct {
	workerGenerator engine.WorkerCreatorFunc
	outputQueue     chan *types.LogBatch
	wg              sync.WaitGroup

	ctx    context.Context
	cancel context.CancelFunc
}

func newSourceSendOnClose(workerGenerator engine.WorkerCreatorFunc) *sourceSendOnClose {
	return &sourceSendOnClose{
		workerGenerator: workerGenerator,
		outputQueue:     make(chan *types.LogBatch, 1),
	}
}

func (s *sourceSendOnClose) Open(ctx context.Context) error {
	if s.ctx != nil {
		panic("context already set")
	}
	if s.cancel != nil {
		panic("cancel function already set")
	}
	s.ctx, s.cancel = context.WithCancel(ctx)

	worker := s.workerGenerator(sourceName, s.outputQueue)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		worker.Run()
	}()
	return nil
}

func (s *sourceSendOnClose) Close() error {
	// Panic if the context is already cancelled
	select {
	case <-s.ctx.Done():
		panic("context already cancelled")
	default:
		// Context is still active, proceed with cancellation
	}

	s.cancel()
	batch := &types.LogBatch{}
	batch.Ack = func() {}
	batch.AddLiterals([]*types.LogLiteral{
		{
			Body: map[string]interface{}{
				"message": "test message",
			},
		},
	})
	s.outputQueue <- batch
	close(s.outputQueue)

	s.wg.Wait()
	return nil
}

type transformerpanicifclosed struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (t *transformerpanicifclosed) Open(ctx context.Context) error {
	if t.ctx != nil {
		panic("context already set")
	}
	if t.cancel != nil {
		panic("cancel function already set")
	}

	t.ctx, t.cancel = context.WithCancel(context.Background())
	return nil
}

func (t *transformerpanicifclosed) Close() error {
	// Panic if the context is already cancelled
	select {
	case <-t.ctx.Done():
		panic("context already cancelled")
	default:
		// Context is still active, proceed with cancellation
	}

	t.cancel()
	return nil
}

func (t *transformerpanicifclosed) Transform(ctx context.Context, batch *types.LogBatch) (*types.LogBatch, error) {
	// Panic if the context is already cancelled
	select {
	case <-t.ctx.Done():
		panic("context already cancelled")
	default:
		// Context is still active, proceed with cancellation
	}

	// This is a no-op transformer
	return batch, nil
}

func (t *transformerpanicifclosed) Name() string {
	return "transformerpanicifclosed"
}
