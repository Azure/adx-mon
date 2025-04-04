package sinks

import (
	"context"
	"fmt"
	"sync"

	"github.com/Azure/adx-mon/collector/logs/types"
)

// CountingSink is intended for testing purposes to consume at least n logs, then close the Done channel.
type CountingSink struct {
	expectedCount int64
	currentCount  int64
	latest        *types.Log

	lock        sync.Mutex
	done        bool
	doneChannel chan int64
	closed      bool
}

func NewCountingSink(expectedCount int64) *CountingSink {
	return &CountingSink{
		expectedCount: expectedCount,
		currentCount:  0,
		latest:        nil,
		done:          false,
		doneChannel:   make(chan int64, 1),
		closed:        false,
	}
}

func (s *CountingSink) Open(ctx context.Context) error {
	return nil
}

func (s *CountingSink) Send(ctx context.Context, batch *types.LogBatch) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	if s.closed {
		return fmt.Errorf("CountingSink is closed")
	}

	s.currentCount += int64(len(batch.Logs))
	s.latest = batch.Logs[len(batch.Logs)-1]
	batch.Ack()
	if !s.done && s.currentCount >= s.expectedCount {
		s.done = true
		s.doneChannel <- s.currentCount
		close(s.doneChannel)
	}
	return nil
}

func (s *CountingSink) Close() error {
	s.lock.Lock()
	defer s.lock.Unlock()
	if s.closed {
		return fmt.Errorf("CountingSink is already closed")
	}
	s.closed = true
	return nil
}

func (s *CountingSink) Name() string {
	return "CountingSink"
}

func (s *CountingSink) DoneChan() chan int64 {
	return s.doneChannel
}

func (s *CountingSink) Latest() *types.Log {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.latest
}
