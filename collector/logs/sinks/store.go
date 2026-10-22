package sinks

import (
	"context"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/storage"
)

type StoreSinkConfig struct {
	Store storage.Store
}

type StoreSink struct {
	store storage.Store
}

func NewStoreSink(config StoreSinkConfig) (*StoreSink, error) {
	return &StoreSink{
		store: config.Store,
	}, nil
}

func (s *StoreSink) Open(ctx context.Context) error {
	return nil
}

func (s *StoreSink) Send(ctx context.Context, batch *types.LogBatch) error {
	err := s.store.WriteNativeLogs(ctx, batch)
	_, failure := storage.SplitOutcome(err)
	if failure == nil {
		batch.Ack()
	}
	return err
}

func (s *StoreSink) Close() error {
	return nil
}

func (s *StoreSink) Name() string {
	return "StoreSink"
}

// Durable reports that this sink owns the durability of the batch: once it
// accepts, the producer must not be asked to resend.
func (s *StoreSink) Durable() bool { return true }
