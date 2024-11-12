//go:build !linux

package journal

import (
	"context"

	"github.com/Azure/adx-mon/pkg/logger"
)

// Dummy impl for all non-linux platforms

type Source struct{}

func New(config SourceConfig) *Source {
	return &Source{}
}

func (s *Source) Open(ctx context.Context) error {
	logger.Warn("journal source is not supported on this platform")
	return nil
}

func (s *Source) Close() error {
	return nil
}

func (s *Source) Name() string {
	return "journal"
}
