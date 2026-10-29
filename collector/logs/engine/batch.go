package engine

import (
	"context"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
)

type BatchConfig struct {
	MaxBatchSize int
	MaxBatchWait time.Duration
	// RealtimeWait returns a shorter wait for a realtime log, or zero for a queued log.
	// Queued-only batches keep the existing periodic MaxBatchWait behavior.
	RealtimeWait func(*types.Log) time.Duration
	InputQueue   <-chan *types.Log
	OutputQueue  chan<- *types.LogBatch
	AckGenerator func(log *types.Log) func()
}

func BatchLogs(ctx context.Context, config BatchConfig) {
	ticker := time.NewTicker(config.MaxBatchWait)
	defer ticker.Stop()
	var realtimeTimer *time.Timer
	var realtimeC <-chan time.Time
	var realtimeDeadline time.Time
	if config.RealtimeWait != nil {
		realtimeTimer = time.NewTimer(time.Hour)
		realtimeTimer.Stop()
		defer realtimeTimer.Stop()
	}
	stopRealtimeTimer := func() {
		if realtimeTimer != nil {
			realtimeTimer.Stop()
		}
		realtimeC = nil
		realtimeDeadline = time.Time{}
	}

	currentBatch := types.LogBatchPool.Get()
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
				currentBatch = types.LogBatchPool.Get()
			}
			stopRealtimeTimer()
		case <-realtimeC:
			if len(currentBatch.Logs) != 0 {
				flush(config, currentBatch)
				currentBatch = types.LogBatchPool.Get()
			}
			stopRealtimeTimer()
		case msg := <-config.InputQueue:
			currentBatch.Logs = append(currentBatch.Logs, msg)
			if len(currentBatch.Logs) >= config.MaxBatchSize {
				flush(config, currentBatch)
				currentBatch = types.LogBatchPool.Get()
				stopRealtimeTimer()
				ticker.Reset(config.MaxBatchWait)
			} else if realtimeTimer != nil {
				if wait := config.RealtimeWait(msg); wait > 0 && wait < config.MaxBatchWait {
					deadline := time.Now().Add(wait)
					if realtimeDeadline.IsZero() || deadline.Before(realtimeDeadline) {
						realtimeDeadline = deadline
						realtimeTimer.Reset(time.Until(deadline))
						realtimeC = realtimeTimer.C
					}
				}
			}
		}
	}
}

func flush(config BatchConfig, currentBatch *types.LogBatch) {
	lastMsg := currentBatch.Logs[len(currentBatch.Logs)-1]
	currentBatch.Ack = config.AckGenerator(lastMsg)
	config.OutputQueue <- currentBatch
}
