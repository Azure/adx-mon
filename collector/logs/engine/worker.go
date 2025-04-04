package engine

import (
	"context"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/metrics"
)

type worker struct {
	SourceName string
	Input      <-chan *types.LogBatch
	Transforms []types.Transformer
	Sink       types.Sink
}

type WorkerCreatorFunc func(string, <-chan *types.LogBatch) *worker

func WorkerCreator(transforms []types.Transformer, sink types.Sink) func(string, <-chan *types.LogBatch) *worker {
	return func(sourceName string, input <-chan *types.LogBatch) *worker {
		return &worker{
			SourceName: sourceName,
			Input:      input,
			Transforms: transforms,
			Sink:       sink,
		}
	}
}

// Run starts the worker and processes incoming log batches.
// It will block until the input channel is closed.
func (w *worker) Run() {
	for msg := range w.Input {
		w.processBatch(context.Background(), msg)
	}
}

func (w *worker) processBatch(ctx context.Context, batch *types.LogBatch) {
	var err error
	for _, transform := range w.Transforms {
		batch, err = transform.Transform(ctx, batch)
		if err != nil {
			metrics.LogsCollectorLogsDropped.WithLabelValues(w.SourceName, transform.Name()).Add(float64(len(batch.Logs)))
			disposeBatch(batch)
			// TODO skip batch if error is not recoverable
			// Nack batch?
			return
		}
	}

	// Freeze the logs in the batch to prevent further modifications
	for _, log := range batch.Logs {
		log.Freeze()
	}
	err = w.Sink.Send(ctx, batch)
	if err != nil {
		metrics.LogsCollectorLogsDropped.WithLabelValues(w.SourceName, w.Sink.Name()).Add(float64(len(batch.Logs)))
		disposeBatch(batch)
		// TODO skip batch if error is not recoverable
		// Nack batch?
		return
	}
	metrics.LogsCollectorLogsSent.WithLabelValues(w.SourceName, w.Sink.Name()).Add(float64(len(batch.Logs)))
	disposeBatch(batch)
}

func disposeBatch(batch *types.LogBatch) {
	for _, log := range batch.Logs {
		types.LogPool.Put(log)
	}
	types.LogBatchPool.Put(batch)
}
