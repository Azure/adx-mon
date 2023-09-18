package sources

import (
	"context"
	"sync"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
)

type ConstSource struct {
	Value         string
	FlushDuration time.Duration
	MaxBatchSize  int

	outputQueue   chan *types.LogBatch
	internalQueue chan string
	closeFn       context.CancelFunc

	wg sync.WaitGroup
}

// TODO more variety of source values
func NewConstSource(value string, flushDuration time.Duration, maxBatchSize int) *ConstSource {
	return &ConstSource{
		Value:         value,
		FlushDuration: flushDuration,
		MaxBatchSize:  maxBatchSize,
		outputQueue:   make(chan *types.LogBatch, 1),
		internalQueue: make(chan string, 1000),
	}
}

func (s *ConstSource) Open(ctx context.Context) error {
	ctx, closeFn := context.WithCancel(ctx)
	s.closeFn = closeFn
	s.wg.Add(1)
	go s.generate(ctx)
	s.wg.Add(1)
	go s.batch(ctx)

	return nil
}

func (s *ConstSource) Close() error {
	s.closeFn()
	s.wg.Wait()
	return nil
}

func (s *ConstSource) Name() string {
	return "ConstSource"
}

func (s *ConstSource) Queue() <-chan *types.LogBatch {
	return s.outputQueue
}

func (s *ConstSource) generate(ctx context.Context) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case s.internalQueue <- s.Value:
		}
	}
}

func (s *ConstSource) batch(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(s.FlushDuration)
	defer ticker.Stop()

	currentBatch := types.LogBatchPool.Get(1024).(*types.LogBatch)
	currentBatch.Reset()
	for {
		select {
		case <-ctx.Done():
			s.outputQueue <- currentBatch
			return
		case <-ticker.C:
			s.outputQueue <- currentBatch
			currentBatch = types.LogBatchPool.Get(1024).(*types.LogBatch)
			currentBatch.Reset()
			ticker.Reset(s.FlushDuration)
		case msg := <-s.internalQueue:
			log := types.LogPool.Get(1).(*types.Log)
			log.Reset()
			log.Timestamp = uint64(time.Now().UnixNano())
			log.ObservedTimestamp = uint64(time.Now().UnixNano())
			log.Body["message"] = msg
			currentBatch.Logs = append(currentBatch.Logs, log)
			if len(currentBatch.Logs) > s.MaxBatchSize {
				s.outputQueue <- currentBatch
				currentBatch = types.LogBatchPool.Get(1024).(*types.LogBatch)
				currentBatch.Reset()
				ticker.Reset(s.FlushDuration)
			}
		}
	}
}
