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

func (w *worker) Run() error {
	for msg := range w.Input {
		w.processBatch(context.Background(), msg)
	}
	return nil
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
