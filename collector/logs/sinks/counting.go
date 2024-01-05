package sinks

import (
	"context"
	"sync"

	"github.com/Azure/adx-mon/collector/logs/types"
)

type CountingSink struct {
	expectedCount int64
	currentCount  int64

	lock        sync.Mutex
	done        bool
	doneChannel chan struct{}
}

func NewCountingSink(expectedCount int64) *CountingSink {
	return &CountingSink{
		expectedCount: expectedCount,
		currentCount:  0,
		done:          false,
		doneChannel:   make(chan struct{}),
	}
}

func (s *CountingSink) Open(ctx context.Context) error {
	return nil
}

func (s *CountingSink) Send(ctx context.Context, batch *types.LogBatch) error {
	s.lock.Lock()
	s.currentCount += int64(len(batch.Logs))
	batch.Ack()
	if !s.done && s.currentCount >= s.expectedCount {
		s.done = true
		close(s.doneChannel)
	}
	s.lock.Unlock()
	return nil
}

func (s *CountingSink) Close() error {
	return nil
}

func (s *CountingSink) Name() string {
	return "CountingSink"
}

func (s *CountingSink) DoneChan() chan struct{} {
	return s.doneChannel
}
