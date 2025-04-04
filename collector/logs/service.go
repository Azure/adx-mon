package logs

import (
	"context"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/service"
)

type Service struct {
	Source     service.Component
	Transforms []types.Transformer
	Sink       types.Sink

	cancel context.CancelFunc
}

func (s *Service) Open(ctx context.Context) error {
	ctx, close := context.WithCancel(ctx)
	s.cancel = close
	// Start from end to front, so that we can close in reverse order.
	if err := s.Sink.Open(ctx); err != nil {
		return err
	}

	for i := len(s.Transforms) - 1; i >= 0; i-- {
		if err := s.Transforms[i].Open(ctx); err != nil {
			return err
		}
	}

	if err := s.Source.Open(ctx); err != nil {
		return err
	}

	return nil
}

func (s *Service) Close() error {
	if err := s.Source.Close(); err != nil {
		logger.Warnf("Failed to close source: %s", err)
	}
	s.cancel()
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
