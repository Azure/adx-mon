package cluster

import (
	"context"

	"github.com/Azure/adx-mon/pkg/logger"
)

type FakeReplicator struct {
	cancelFn context.CancelFunc
	queue    chan *Batch
}

func NewFakeReplicator() *FakeReplicator {
	return &FakeReplicator{
		queue: make(chan *Batch, 10000),
	}
}

func (f *FakeReplicator) Open(ctx context.Context) error {
	ctx, f.cancelFn = context.WithCancel(ctx)
	go f.replicate(ctx)
	return nil
}

func (f *FakeReplicator) Close() error {
	f.cancelFn()
	return nil
}

func (f *FakeReplicator) replicate(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		batch := <-f.queue

		for _, seg := range batch.Segments {
			if logger.IsDebug() {
				logger.Debugf("Transferred %s", seg.Path)
			}
		}
		if err := batch.Remove(); err != nil {
			logger.Errorf("Failed to remove batch: %s", err.Error())
		}
		batch.Release()
	}
}

func (f *FakeReplicator) TransferQueue() chan *Batch {
	return f.queue
}
