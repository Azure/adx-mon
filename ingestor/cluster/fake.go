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

type fakeSegmentRemover struct {
}

func (f fakeSegmentRemover) Remove(path string) error {
	return nil
}

type fakeBatcher struct{}

func (f fakeBatcher) Open(ctx context.Context) error { return nil }
func (f fakeBatcher) Close() error                   { return nil }
func (f fakeBatcher) BatchSegments() error           { return nil }
func (f fakeBatcher) UploadQueueSize() int           { return 0 }
func (f fakeBatcher) TransferQueueSize() int         { return 0 }
func (f fakeBatcher) SegmentsTotal() int64           { return 0 }
func (f fakeBatcher) SegmentsSize() int64            { return 0 }
func (f fakeBatcher) Release(batch *Batch)           {}
func (f fakeBatcher) Remove(batch *Batch) error      { return nil }
