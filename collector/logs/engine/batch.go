package engine

import (
	"context"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/wal"
)

type BatchConfig struct {
	MaxBatchSize int
	MaxBatchWait time.Duration
	InputQueue   <-chan *types.Log
	OutputQueue  chan<- *types.LogBatch
	AckGenerator func(log *types.Log) func()
	SampleType   wal.SampleType
}

func BatchLogs(ctx context.Context, config BatchConfig) {
	ticker := time.NewTicker(config.MaxBatchWait)
	defer ticker.Stop()

	currentBatch := types.LogBatchPool.Get(1024).(*types.LogBatch)
	currentBatch.Reset()
	currentBatch.SampleType = config.SampleType
	for {
		select {
		case <-ctx.Done():
			if len(currentBatch.Logs) != 0 {
				flush(config, currentBatch)
			}
			close(config.OutputQueue)
			return
		case <-ticker.C:
			if len(currentBatch.Logs) != 0 {
				flush(config, currentBatch)
				currentBatch = types.LogBatchPool.Get(1024).(*types.LogBatch)
				currentBatch.Reset()
				currentBatch.SampleType = config.SampleType
			}
		case msg := <-config.InputQueue:
			currentBatch.Logs = append(currentBatch.Logs, msg)
			if len(currentBatch.Logs) >= config.MaxBatchSize {
				flush(config, currentBatch)
				currentBatch = types.LogBatchPool.Get(1024).(*types.LogBatch)
				currentBatch.Reset()
				currentBatch.SampleType = config.SampleType
				ticker.Reset(config.MaxBatchWait)
			}
		}
	}
}

func flush(config BatchConfig, currentBatch *types.LogBatch) {
	lastMsg := currentBatch.Logs[len(currentBatch.Logs)-1]
	currentBatch.Ack = config.AckGenerator(lastMsg)
	config.OutputQueue <- currentBatch
}
