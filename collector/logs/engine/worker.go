package engine

import (
	"context"
	"sync"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/storage"
)

type worker struct {
	SourceName   string
	Input        <-chan *types.LogBatch
	Transforms   []types.Transformer
	Sinks        []types.Sink
	BatchTimeout time.Duration
}

type WorkerCreatorFunc func(string, <-chan *types.LogBatch) *worker

func WorkerCreator(transforms []types.Transformer, sinks []types.Sink) func(string, <-chan *types.LogBatch) *worker {
	return func(sourceName string, input <-chan *types.LogBatch) *worker {
		return &worker{
			SourceName:   sourceName,
			Input:        input,
			Transforms:   transforms,
			Sinks:        sinks,
			BatchTimeout: 10 * time.Second,
		}
	}
}

// Run starts the worker and processes incoming log batches.
// It will block until the input channel is closed.
func (w *worker) Run() {
	w.RunWaitingForStorage(nil)
}

// RunWaitingForStorage is like Run, but when storage is full the durable sink waits for room rather than dropping
// the batch, until stop is done.  Sources that stop reading while the worker waits use it, so full storage delays
// their logs instead of losing them.
//
// Once a batch fails after stop is done, the batches still queued behind it are disposed of without being sent or
// acknowledged.  Acknowledgements only move a source's cursor forward, so storing and acknowledging a later batch would
// move the cursor past the logs that failed, and they would not be read again on restart.
func (w *worker) RunWaitingForStorage(stop context.Context) {
	stopped := false
	for msg := range w.Input {
		if stopped {
			types.DisposeLogBatch(msg)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), w.BatchTimeout)
		if stop != nil {
			ctx = types.WithStorageWait(ctx, stop)
		}
		_, err := w.ProcessBatch(ctx, msg)
		cancel()
		stopped = err != nil && stop != nil && stop.Err() != nil
	}
}

func (w *worker) processBatch(ctx context.Context, batch *types.LogBatch) {
	_, _ = w.ProcessBatch(ctx, batch)
}

// ProcessBatch owns and disposes the batch and waits for admission at the sinks.
// HTTP callers use its result to report quota shedding before acknowledging.
//
// Only durable sinks determine the returned error. An optional exporter that
// fails is counted and logged, but it must not make the producer resend records
// the durable sink already stored. When no sink claims durability every sink is
// treated as authoritative, so data is never acknowledged as stored when it
// went nowhere.
func (w *worker) ProcessBatch(ctx context.Context, batch *types.LogBatch) (int64, error) {
	var err error
	for _, transform := range w.Transforms {
		batch, err = transform.Transform(ctx, batch)
		if err != nil {
			logger.Warnf("Failed to transform logs from source %s -> %s: %v", w.SourceName, transform.Name(), err)
			metrics.LogsCollectorLogsDropped.WithLabelValues(w.SourceName, transform.Name()).Add(float64(len(batch.Logs)))
			types.DisposeLogBatch(batch)
			return 0, err
		}
	}

	// Freeze the logs in the batch to prevent further modifications
	for _, log := range batch.Logs {
		log.Freeze()
	}

	var durableSinks int
	for _, sink := range w.Sinks {
		if types.IsDurable(sink) {
			durableSinks++
		}
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var rejected int64
	var failure error
	wg.Add(len(w.Sinks))
	for _, sink := range w.Sinks {
		go func(sink types.Sink) {
			defer wg.Done()
			n, err := sendToSink(ctx, sink, batch, w.SourceName)

			authoritative := durableSinks == 0 || types.IsDurable(sink)
			mu.Lock()
			if authoritative {
				if n > rejected {
					rejected = n
				}
				if err != nil {
					failure = err
				}
			}
			mu.Unlock()
		}(sink)
	}

	wg.Wait()
	types.DisposeLogBatch(batch)
	return rejected, failure
}

func sendToSink(ctx context.Context, sink types.Sink, batch *types.LogBatch, sourceName string) (int64, error) {
	err := sink.Send(ctx, batch)
	outcome, failure := storage.SplitOutcome(err)
	if failure != nil {
		logger.Warnf("Failed to send logs to sink %s -> %s: %v", sourceName, sink.Name(), err)
		metrics.LogsCollectorLogsDropped.WithLabelValues(sourceName, sink.Name()).Add(float64(len(batch.Logs)))
		return outcome.Rejected, failure
	}
	metrics.LogsCollectorLogsDropped.WithLabelValues(sourceName, sink.Name()).Add(float64(outcome.Rejected))
	if sent := int64(len(batch.Logs)) - outcome.Rejected; sent > 0 {
		metrics.LogsCollectorLogsSent.WithLabelValues(sourceName, sink.Name()).Add(float64(sent))
	}
	return outcome.Rejected, nil
}
