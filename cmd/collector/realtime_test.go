// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package main

import (
	"testing"

	"github.com/Azure/adx-mon/cmd/collector/config"
	"github.com/Azure/adx-mon/collector"
	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/stretchr/testify/require"
)

func TestRealtimeOpts_Unset(t *testing.T) {
	opts, percent, err := realtimeOpts(nil)
	require.NoError(t, err)
	require.Nil(t, opts)
	require.Zero(t, percent)
}

func TestRealtimeOpts_NoTables(t *testing.T) {
	cfg := config.Config{Realtime: &config.Realtime{QueuedReservedWorkersPercent: 20}}
	require.NoError(t, cfg.Validate())

	opts, percent, err := realtimeOpts(cfg.Realtime)
	require.NoError(t, err)
	require.Nil(t, opts)
	require.Equal(t, 20, percent)
}

func TestRealtimeOpts_Tables(t *testing.T) {
	cfg := config.Config{Realtime: &config.Realtime{
		Tables: []*config.RealtimeTable{{Database: "Logs", Table: "Realtime"}},
	}}
	require.NoError(t, cfg.Validate())

	opts, percent, err := realtimeOpts(cfg.Realtime)
	require.NoError(t, err)
	require.NotNil(t, opts)
	require.Equal(t, config.DefaultRealtimeQueuedReservedWorkersPercent, percent)
	require.Equal(t, ingestpolicy.PriorityRealtime, opts.Policy.Priority("Logs", "Realtime"))
	require.Equal(t, config.DefaultRealtimeMaxSegmentAge, opts.MaxSegmentAge)
	require.Equal(t, config.DefaultRealtimeMaxBatchLatency, opts.MaxBatchLatency)
	require.Equal(t, config.DefaultRealtimeMaxBatchBytes, opts.MaxBatchBytes)
	require.Equal(t, config.DefaultRealtimeReservedDiskBytes, opts.ReservedDiskBytes)
}

func TestDefaultMaxDiskUsageMatchesCollector(t *testing.T) {
	// The config validates the realtime reservation against the collector's default max disk usage.
	require.Equal(t, collector.DefaultMaxDiskUsage, config.DefaultMaxDiskUsage)
}
