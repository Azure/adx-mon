package sources

import (
	"context"
	"sync"
	"time"

	"github.com/Azure/adx-mon/collector/logs/engine"
	"github.com/Azure/adx-mon/collector/logs/types"
)

type ConstSource struct {
	Value         string
	FlushDuration time.Duration
	MaxBatchSize  int

	outputQueue     chan *types.LogBatch
	internalQueue   chan *types.Log
	workerGenerator engine.WorkerCreatorFunc
	closeFn         context.CancelFunc

	wg sync.WaitGroup
}

// TODO more variety of source values
func NewConstSource(value string, flushDuration time.Duration, maxBatchSize int, workerGenerator engine.WorkerCreatorFunc) *ConstSource {
	return &ConstSource{
		Value:           value,
		FlushDuration:   flushDuration,
		MaxBatchSize:    maxBatchSize,
		workerGenerator: workerGenerator,
		outputQueue:     make(chan *types.LogBatch, 1),
		internalQueue:   make(chan *types.Log, 1000),
	}
}

func (s *ConstSource) Open(ctx context.Context) error {
	ctx, closeFn := context.WithCancel(ctx)
	s.closeFn = closeFn

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.generate(ctx)
	}()
	config := engine.BatchConfig{
		MaxBatchSize: s.MaxBatchSize,
		MaxBatchWait: s.FlushDuration,
		InputQueue:   s.internalQueue,
		OutputQueue:  s.outputQueue,
		AckGenerator: func(log *types.Log) func() {
			return func() {
			}
		},
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		engine.BatchLogs(ctx, config)
	}()

	worker := s.workerGenerator("ConstSource", s.outputQueue)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		worker.Run()
	}()

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

func (s *ConstSource) generate(ctx context.Context) {
	for {
		log := types.LogPool.Get(1).(*types.Log)
		log.Reset()
		log.Timestamp = uint64(time.Now().UnixNano())
		log.ObservedTimestamp = uint64(time.Now().UnixNano())
		log.Body["message"] = s.Value

		select {
		case <-ctx.Done():
			return
		case s.internalQueue <- log:
		}
	}
}
