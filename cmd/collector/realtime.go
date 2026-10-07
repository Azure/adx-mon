// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package main

import (
	"github.com/Azure/adx-mon/cmd/collector/config"
	"github.com/Azure/adx-mon/collector"
)

// realtimeOpts returns the collector's realtime options and the percentage of transfer workers reserved for queued
// batches.  The options are nil if no realtime tables are configured.  cfg must be validated.
func realtimeOpts(cfg *config.Realtime) (*collector.RealtimeOpts, int, error) {
	if cfg == nil {
		return nil, 0, nil
	}

	policy, err := cfg.Policy()
	if err != nil {
		return nil, 0, err
	}
	if !policy.HasRealtime() {
		return nil, cfg.QueuedReservedWorkersPercent, nil
	}

	return &collector.RealtimeOpts{
		Policy:            policy,
		MaxSegmentAge:     cfg.MaxSegmentAge(),
		MaxBatchLatency:   cfg.MaxBatchLatency(),
		MaxBatchBytes:     cfg.MaxBatchBytes,
		ReservedDiskBytes: cfg.ReservedDiskBytes,
	}, cfg.QueuedReservedWorkersPercent, nil
}
