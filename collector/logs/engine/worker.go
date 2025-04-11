package engine

import (
	"context"
	"sync"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
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
	for msg := range w.Input {
		ctx, cancel := context.WithTimeout(context.Background(), w.BatchTimeout)
		w.processBatch(ctx, msg)
		cancel()
	}
}

func (w *worker) processBatch(ctx context.Context, batch *types.LogBatch) {
	var err error
	for _, transform := range w.Transforms {
		batch, err = transform.Transform(ctx, batch)
		if err != nil {
			logger.Warnf("Failed to transform logs from source %s -> %s: %v", w.SourceName, transform.Name(), err)
			metrics.LogsCollectorLogsDropped.WithLabelValues(w.SourceName, transform.Name()).Add(float64(len(batch.Logs)))
			disposeBatch(batch)
			return
		}
	}

	// Freeze the logs in the batch to prevent further modifications
	for _, log := range batch.Logs {
		log.Freeze()
	}

	var wg sync.WaitGroup
	wg.Add(len(w.Sinks))
	for _, sink := range w.Sinks {
		go func(sink types.Sink) {
			defer wg.Done()

			err = sink.Send(ctx, batch)
			if err != nil {
				logger.Warnf("Failed to send logs to sink %s -> %s: %v", w.SourceName, sink.Name(), err)
				metrics.LogsCollectorLogsDropped.WithLabelValues(w.SourceName, sink.Name()).Add(float64(len(batch.Logs)))
				return
			}
			metrics.LogsCollectorLogsSent.WithLabelValues(w.SourceName, sink.Name()).Add(float64(len(batch.Logs)))
		}(sink)
	}
	wg.Wait()
	disposeBatch(batch)
}

func disposeBatch(batch *types.LogBatch) {
	for _, log := range batch.Logs {
		types.LogPool.Put(log)
	}
	types.LogBatchPool.Put(batch)
}
