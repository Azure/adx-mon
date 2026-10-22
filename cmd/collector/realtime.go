// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package main

import (
	"github.com/Azure/adx-mon/cmd/collector/config"
	"github.com/Azure/adx-mon/collector"
)

// realtimeOpts returns the collector's realtime options and the percentage of transfer workers reserved for queued
// batches. The options always contain a policy, which is empty when no realtime tables are configured. cfg must be
// validated.
func realtimeOpts(cfg *config.Realtime) (*collector.RealtimeOpts, int, error) {
	policy, err := cfg.Policy()
	if err != nil {
		return nil, 0, err
	}

	queuedReservedWorkersPercent := config.DefaultRealtimeQueuedReservedWorkersPercent
	if cfg != nil && cfg.QueuedReservedWorkersPercent != 0 {
		queuedReservedWorkersPercent = cfg.QueuedReservedWorkersPercent
	}
	options := &collector.RealtimeOpts{Policy: policy}
	if !policy.HasRealtime() {
		return options, queuedReservedWorkersPercent, nil
	}

	return &collector.RealtimeOpts{
		Policy:            policy,
		MaxSegmentAge:     cfg.MaxSegmentAge(),
		MaxBatchLatency:   cfg.MaxBatchLatency(),
		LogBatchWait:      cfg.LogBatchWait(),
		MaxBatchBytes:     cfg.MaxBatchBytes,
		ReservedDiskBytes: cfg.ReservedDiskBytes,
	}, queuedReservedWorkersPercent, nil
}
