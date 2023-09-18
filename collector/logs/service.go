package logs

import (
	"context"
	"sync"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
)

type Service struct {
	Source     types.Source
	Transforms []types.Transformer
	Sink       types.Sink

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

func (s *Service) Open(ctx context.Context) error {
	ctx, close := context.WithCancel(ctx)
	s.cancel = close
	// Start from end to front, so that we can close in reverse order.
	if err := s.Sink.Open(ctx); err != nil {
		return err
	}
	defer s.Sink.Close()

	for i := len(s.Transforms) - 1; i >= 0; i-- {
		if err := s.Transforms[i].Open(ctx); err != nil {
			return err
		}
		defer s.Transforms[i].Close()
	}

	if err := s.Source.Open(ctx); err != nil {
		return err
	}

	s.wg.Add(1)
	go s.process(ctx)
	return nil
}

func (s *Service) Close() error {
	if err := s.Source.Close(); err != nil {
		logger.Warnf("Failed to close source: %s", err)
	}
	s.cancel()
	s.wg.Wait()
	for _, transform := range s.Transforms {
		if err := transform.Close(); err != nil {
			logger.Warnf("Failed to close transform: %s", err)
		}
	}
	if err := s.Sink.Close(); err != nil {
		logger.Warnf("Failed to close sink: %s", err)
	}

	return nil
}

func (s *Service) process(ctx context.Context) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			// TODO ensure we have flushed.
			return
		case batch := <-s.Source.Queue():
			// TODO curry labels?
			metrics.LogsCollectorLogsCollected.WithLabelValues(s.Source.Name()).Add(float64(len(batch.Logs)))
			s.processBatch(ctx, batch)
		}
	}
}

func (s *Service) processBatch(ctx context.Context, batch *types.LogBatch) {
	var err error
	for _, transform := range s.Transforms {
		batch, err = transform.Transform(ctx, batch)
		if err != nil {
			metrics.LogsCollectorLogsDropped.WithLabelValues(s.Source.Name(), transform.Name()).Add(float64(len(batch.Logs)))
			s.disposeBatch(batch)
			// TODO skip batch if error is not recoverable
			// Nack batch?
			return
		}
	}
	err = s.Sink.Send(ctx, batch)
	if err != nil {
		metrics.LogsCollectorLogsDropped.WithLabelValues(s.Source.Name(), s.Sink.Name()).Add(float64(len(batch.Logs)))
		s.disposeBatch(batch)
		// TODO skip batch if error is not recoverable
		// Nack batch?
		return
	}
	metrics.LogsCollectorLogsSent.WithLabelValues(s.Source.Name(), s.Sink.Name()).Add(float64(len(batch.Logs)))
	s.disposeBatch(batch)
}

func (s *Service) disposeBatch(batch *types.LogBatch) {
	for _, log := range batch.Logs {
		types.LogPool.Put(log)
	}
	types.LogBatchPool.Put(batch)
}
