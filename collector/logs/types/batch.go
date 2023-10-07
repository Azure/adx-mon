package types

import (
	"context"
	"time"
)

type BatchConfig struct {
	MaxBatchSize int
	MaxBatchWait time.Duration
	InputQueue   <-chan *Log
	OutputQueue  chan<- *LogBatch
	AckGenerator func(log *Log) func()
}

func BatchLogs(ctx context.Context, config BatchConfig) error {
	ticker := time.NewTicker(config.MaxBatchWait)
	defer ticker.Stop()

	currentBatch := LogBatchPool.Get(1024).(*LogBatch)
	currentBatch.Reset()
	for {
		select {
		case <-ctx.Done():
			config.OutputQueue <- currentBatch
			return nil
		case <-ticker.C:
			if len(currentBatch.Logs) != 0 {
				config.OutputQueue <- currentBatch
				currentBatch = LogBatchPool.Get(1024).(*LogBatch)
				currentBatch.Reset()
			}
		case msg := <-config.InputQueue:
			currentBatch.Logs = append(currentBatch.Logs, msg)
			currentBatch.Ack = config.AckGenerator(msg)
			if len(currentBatch.Logs) >= config.MaxBatchSize {
				config.OutputQueue <- currentBatch
				currentBatch = LogBatchPool.Get(1024).(*LogBatch)
				currentBatch.Reset()
				ticker.Reset(config.MaxBatchWait)
			}
		}
	}
}
