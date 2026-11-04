package sinks

import (
	"context"
	"errors"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/logger"
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
	if stop := types.StorageWait(ctx); stop != nil {
		err = s.waitForStorage(ctx, stop, batch, err)
	}
	_, failure := storage.SplitOutcome(err)
	if failure == nil {
		batch.Ack()
	}
	return err
}

// waitForStorage retries the logs that were not written because storage was full until they are written or stop is
// done.  Logs written before storage filled are not written again.
func (s *StoreSink) waitForStorage(ctx, stop context.Context, batch *types.LogBatch, err error) error {
	remaining := batch.Logs
	delay := 100 * time.Millisecond
	for {
		var partial *storage.PartialWriteError
		if !errors.As(err, &partial) {
			return err
		}
		if delay == 100*time.Millisecond {
			logger.Warnf("Storage is full, waiting to store %d logs", len(remaining)-partial.Written)
		}
		remaining = remaining[partial.Written:]
		select {
		case <-stop.Done():
			return err
		case <-time.After(delay):
		}
		delay = min(2*delay, 5*time.Second)
		err = s.store.WriteNativeLogs(context.WithoutCancel(ctx), &types.LogBatch{Logs: remaining})
	}
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
