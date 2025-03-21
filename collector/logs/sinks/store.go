package sinks

import (
	"context"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/storage"
)

type StoreSinkConfig struct {
	Store         storage.Store
	AddAttributes map[string]string
}

type StoreSink struct {
	store         storage.Store
	addAttributes map[string]string
}

func NewStoreSink(config StoreSinkConfig) (*StoreSink, error) {
	return &StoreSink{
		store:         config.Store,
		addAttributes: config.AddAttributes,
	}, nil
}

func (s *StoreSink) Open(ctx context.Context) error {
	return nil
}

func (s *StoreSink) Send(ctx context.Context, batch *types.LogBatch) error {
	for _, l := range batch.Logs {
		for k, v := range s.addAttributes {
			l.SetResourceValue(k, v)
		}
	}
	err := s.store.WriteNativeLogs(ctx, batch)
	if err != nil {
		return err
	}
	batch.Ack()
	return nil
}

func (s *StoreSink) Close() error {
	return nil
}

func (s *StoreSink) Name() string {
	return "StoreSink"
}
