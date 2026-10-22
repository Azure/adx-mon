package engine

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
)

func TestBatchLogsRealtimeWait(t *testing.T) {
	input := make(chan *types.Log)
	output := make(chan *types.LogBatch, 4)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		BatchLogs(ctx, BatchConfig{
			MaxBatchSize: 1000,
			MaxBatchWait: 500 * time.Millisecond,
			RealtimeWait: func(log *types.Log) time.Duration {
				if value, ok := log.GetAttributeValue("priority"); ok && value == "realtime" {
					return 50 * time.Millisecond
				}
				return 0
			},
			InputQueue:   input,
			OutputQueue:  output,
			AckGenerator: func(*types.Log) func() { return func() {} },
		})
	}()
	defer func() {
		cancel()
		<-done
		for batch := range output {
			types.DisposeLogBatch(batch)
		}
	}()

	queued := types.NewLog()
	queued.SetAttributeValue("priority", "queued")
	input <- queued
	select {
	case batch := <-output:
		types.DisposeLogBatch(batch)
		t.Fatal("queued log was flushed before the normal batch interval")
	case <-time.After(100 * time.Millisecond):
	}

	realtime := types.NewLog()
	realtime.SetAttributeValue("priority", "realtime")
	start := time.Now()
	input <- realtime
	select {
	case batch := <-output:
		if elapsed := time.Since(start); elapsed > 350*time.Millisecond {
			t.Errorf("realtime batch took %s, expected less than queued interval", elapsed)
		}
		if len(batch.Logs) != 2 {
			t.Errorf("batch contains %d logs, want queued and realtime", len(batch.Logs))
		}
		types.DisposeLogBatch(batch)
	case <-time.After(400 * time.Millisecond):
		t.Fatal("realtime log was not flushed promptly")
	}
}

func TestBatchLogsQueuedWaitWithRealtimePolicy(t *testing.T) {
	input := make(chan *types.Log)
	output := make(chan *types.LogBatch, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		BatchLogs(ctx, BatchConfig{
			MaxBatchSize: 1000,
			MaxBatchWait: 250 * time.Millisecond,
			RealtimeWait: func(*types.Log) time.Duration { return 0 },
			InputQueue:   input,
			OutputQueue:  output,
			AckGenerator: func(*types.Log) func() { return func() {} },
		})
	}()
	defer func() { cancel(); <-done }()

	input <- types.NewLog()
	select {
	case batch := <-output:
		types.DisposeLogBatch(batch)
		t.Fatal("queued-only batch flushed on realtime interval")
	case <-time.After(75 * time.Millisecond):
	}
	select {
	case batch := <-output:
		types.DisposeLogBatch(batch)
	case <-time.After(time.Second):
		t.Fatal("queued batch did not flush on normal ticker")
	}
}
