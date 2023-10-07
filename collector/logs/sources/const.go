package sources

import (
	"context"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"golang.org/x/sync/errgroup"
)

type ConstSource struct {
	Value         string
	FlushDuration time.Duration
	MaxBatchSize  int

	outputQueue   chan *types.LogBatch
	internalQueue chan *types.Log
	closeFn       context.CancelFunc

	errGroup *errgroup.Group
}

// TODO more variety of source values
func NewConstSource(value string, flushDuration time.Duration, maxBatchSize int) *ConstSource {
	return &ConstSource{
		Value:         value,
		FlushDuration: flushDuration,
		MaxBatchSize:  maxBatchSize,
		outputQueue:   make(chan *types.LogBatch, 1),
		internalQueue: make(chan *types.Log, 1000),
	}
}

func (s *ConstSource) Open(ctx context.Context) error {
	ctx, closeFn := context.WithCancel(ctx)
	s.closeFn = closeFn
	group, groupCtx := errgroup.WithContext(ctx)
	s.errGroup = group

	group.Go(func() error {
		return s.generate(groupCtx)
	})
	config := types.BatchConfig{
		MaxBatchSize: s.MaxBatchSize,
		MaxBatchWait: s.FlushDuration,
		InputQueue:   s.internalQueue,
		OutputQueue:  s.outputQueue,
		AckGenerator: func(log *types.Log) func() {
			return func() {
			}
		},
	}
	group.Go(func() error {
		return types.BatchLogs(groupCtx, config)
	})

	return nil
}

func (s *ConstSource) Close() error {
	s.closeFn()
	s.errGroup.Wait()
	return nil
}

func (s *ConstSource) Name() string {
	return "ConstSource"
}

func (s *ConstSource) Queue() <-chan *types.LogBatch {
	return s.outputQueue
}

func (s *ConstSource) generate(ctx context.Context) error {
	for {
		log := types.LogPool.Get(1).(*types.Log)
		log.Reset()
		log.Timestamp = uint64(time.Now().UnixNano())
		log.ObservedTimestamp = uint64(time.Now().UnixNano())
		log.Body["message"] = s.Value

		select {
		case <-ctx.Done():
			return nil
		case s.internalQueue <- log:
		}
	}
}
