// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package main

import (
	"testing"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/storage"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

const (
	testMetricsEndpoint = "https://metrics.kusto.windows.net"
	testLogsEndpoint    = "https://logs.kusto.windows.net"
	testMaxDiskUsage    = 10 * 1024 * 1024 * 1024
)

var testDatabaseEndpoints = map[string]string{
	"Metrics": testMetricsEndpoint,
	"Logs":    testLogsEndpoint,
}

func defaultRealtimeFlags() realtimeFlags {
	return realtimeFlags{
		MinSlots:                     defaultRealtimeMinSlots,
		MaxSegmentAge:                defaultRealtimeMaxSegmentAge,
		MaxBatchLatency:              defaultRealtimeMaxBatchLatency,
		MaxBatchBytes:                defaultRealtimeMaxBatchBytes,
		MaxLag:                       defaultRealtimeMaxLag,
		ReservedDiskBytes:            defaultRealtimeReservedDiskBytes,
		QueuedReservedWorkersPercent: defaultQueuedReservedWorkersPercent,
	}
}

func TestParseRealtimeConfig_Disabled(t *testing.T) {
	cfg, err := parseRealtimeConfig(defaultRealtimeFlags(), testDatabaseEndpoints, testMaxDiskUsage, storage.BackendADX)
	require.NoError(t, err)
	require.False(t, cfg.Policy.HasRealtime())
	require.Empty(t, cfg.StreamingBudgets)
	// No disk is reserved when realtime is not used so existing queued capacity is unchanged.
	require.Zero(t, cfg.ReservedDiskBytes)
}

func TestParseRealtimeConfig_DisabledIgnoresBackendAndDiskUsage(t *testing.T) {
	// Existing deployments with small disks or ClickHouse must not fail on realtime defaults.
	_, err := parseRealtimeConfig(defaultRealtimeFlags(), nil, 1024, storage.BackendClickHouse)
	require.NoError(t, err)
}

func TestParseRealtimeConfig_Enabled(t *testing.T) {
	f := defaultRealtimeFlags()
	f.Tables = []string{"Metrics.CpuUsage", " Logs.ApplicationErrors "}
	f.StreamingBudgets = []string{testMetricsEndpoint + "=100", "HTTPS://LOGS.kusto.windows.net/=20"}
	f.MinSlots = 2
	f.MaxSlots = 8

	cfg, err := parseRealtimeConfig(f, testDatabaseEndpoints, testMaxDiskUsage, storage.BackendADX)
	require.NoError(t, err)
	require.Equal(t, ingestpolicy.PriorityRealtime, cfg.Policy.Priority("Metrics", "CpuUsage"))
	require.Equal(t, ingestpolicy.PriorityRealtime, cfg.Policy.Priority("Logs", "ApplicationErrors"))
	require.Equal(t, ingestpolicy.PriorityQueued, cfg.Policy.Priority("Metrics", "MemoryUsage"))
	require.Equal(t, map[string]int{
		"https://metrics.kusto.windows.net": 100,
		"https://logs.kusto.windows.net":    20,
	}, cfg.StreamingBudgets)
	require.Equal(t, 2, cfg.MinSlots)
	require.Equal(t, 8, cfg.MaxSlots)
	require.Equal(t, defaultRealtimeReservedDiskBytes, cfg.ReservedDiskBytes)
}

func TestParseRealtimeConfig_NormalizesDatabase(t *testing.T) {
	f := defaultRealtimeFlags()
	f.Tables = []string{"My-Metrics.Cpu_Usage"}
	f.StreamingBudgets = []string{testMetricsEndpoint + "=10"}

	cfg, err := parseRealtimeConfig(f, map[string]string{"My-Metrics": testMetricsEndpoint}, testMaxDiskUsage, storage.BackendADX)
	require.NoError(t, err)
	require.Equal(t, ingestpolicy.PriorityRealtime, cfg.Policy.Priority("MyMetrics", "CpuUsage"))
}

func TestParseRealtimeConfig_Invalid(t *testing.T) {
	tests := []struct {
		name     string
		modify   func(f *realtimeFlags)
		endpts   map[string]string
		backend  storage.Backend
		contains string
	}{
		{name: "min slots zero", modify: func(f *realtimeFlags) { f.MinSlots = 0 }, contains: "--realtime-min-slots"},
		{name: "max slots negative", modify: func(f *realtimeFlags) { f.MaxSlots = -1 }, contains: "--realtime-max-slots must not be negative"},
		{name: "max below min", modify: func(f *realtimeFlags) { f.MinSlots = 4; f.MaxSlots = 2 }, contains: "must be 0 or at least --realtime-min-slots"},
		{name: "segment age zero", modify: func(f *realtimeFlags) { f.MaxSegmentAge = 0 }, contains: "--realtime-max-segment-age"},
		{name: "batch latency zero", modify: func(f *realtimeFlags) { f.MaxBatchLatency = 0 }, contains: "--realtime-max-batch-latency"},
		{name: "batch bytes zero", modify: func(f *realtimeFlags) { f.MaxBatchBytes = 0 }, contains: "--realtime-max-batch-bytes"},
		{name: "batch bytes over streaming limit", modify: func(f *realtimeFlags) { f.MaxBatchBytes = maxStreamingRequestBytes + 1 }, contains: "--realtime-max-batch-bytes"},
		{name: "max lag zero", modify: func(f *realtimeFlags) { f.MaxLag = 0 }, contains: "--realtime-max-lag"},
		{name: "reserved disk negative", modify: func(f *realtimeFlags) { f.ReservedDiskBytes = -1 }, contains: "--realtime-reserved-disk-bytes must not be negative"},
		{name: "reserved workers zero", modify: func(f *realtimeFlags) { f.QueuedReservedWorkersPercent = 0 }, contains: "between 1 and 99"},
		{name: "reserved workers all", modify: func(f *realtimeFlags) { f.QueuedReservedWorkersPercent = 100 }, contains: "between 1 and 99"},
		{name: "table missing dot", modify: func(f *realtimeFlags) { f.Tables = []string{"MetricsCpuUsage"} }, contains: "expected <db>.<table>"},
		{name: "table extra dot", modify: func(f *realtimeFlags) { f.Tables = []string{"a.b.c"} }, contains: "expected <db>.<table>"},
		{name: "table empty part", modify: func(f *realtimeFlags) { f.Tables = []string{"Metrics."} }, contains: "expected <db>.<table>"},
		{
			name: "duplicate table",
			modify: func(f *realtimeFlags) {
				f.Tables = []string{"Metrics.CpuUsage", "Metrics.Cpu_Usage"}
				f.StreamingBudgets = []string{testMetricsEndpoint + "=10"}
			},
			contains: "duplicate realtime table",
		},
		{
			name: "unknown database",
			modify: func(f *realtimeFlags) {
				f.Tables = []string{"Other.CpuUsage"}
				f.StreamingBudgets = []string{testMetricsEndpoint + "=10"}
			},
			contains: "not a configured metrics or logs database",
		},
		{
			name:     "missing budget",
			modify:   func(f *realtimeFlags) { f.Tables = []string{"Metrics.CpuUsage"} },
			contains: `no --realtime-streaming-budget for endpoint "https://metrics.kusto.windows.net"`,
		},
		{
			name: "budget for other endpoint only",
			modify: func(f *realtimeFlags) {
				f.Tables = []string{"Metrics.CpuUsage"}
				f.StreamingBudgets = []string{testLogsEndpoint + "=10"}
			},
			contains: "no --realtime-streaming-budget",
		},
		{name: "budget missing equals", modify: func(f *realtimeFlags) { f.StreamingBudgets = []string{testMetricsEndpoint} }, contains: "expected <endpoint>=<n>"},
		{name: "budget missing endpoint", modify: func(f *realtimeFlags) { f.StreamingBudgets = []string{"=10"} }, contains: "endpoint is required"},
		{name: "budget not a number", modify: func(f *realtimeFlags) { f.StreamingBudgets = []string{testMetricsEndpoint + "=abc"} }, contains: "positive integer"},
		{name: "budget zero", modify: func(f *realtimeFlags) { f.StreamingBudgets = []string{testMetricsEndpoint + "=0"} }, contains: "positive integer"},
		{name: "budget unknown endpoint", modify: func(f *realtimeFlags) { f.StreamingBudgets = []string{"https://typo.kusto.windows.net=10"} }, contains: "is not a configured kusto endpoint"},
		{
			name: "budget duplicate endpoint",
			modify: func(f *realtimeFlags) {
				f.StreamingBudgets = []string{testMetricsEndpoint + "=10", testMetricsEndpoint + "/=20"}
			},
			contains: "is set more than once",
		},
		{
			name: "clickhouse backend",
			modify: func(f *realtimeFlags) {
				f.Tables = []string{"Metrics.CpuUsage"}
				f.StreamingBudgets = []string{testMetricsEndpoint + "=10"}
			},
			backend:  storage.BackendClickHouse,
			contains: `only supported with storage backend "adx"`,
		},
		{
			name: "reserved disk not below max disk usage",
			modify: func(f *realtimeFlags) {
				f.Tables = []string{"Metrics.CpuUsage"}
				f.StreamingBudgets = []string{testMetricsEndpoint + "=10"}
				f.ReservedDiskBytes = testMaxDiskUsage
			},
			contains: "must be less than --max-disk-usage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := defaultRealtimeFlags()
			tt.modify(&f)
			endpts := tt.endpts
			if endpts == nil {
				endpts = testDatabaseEndpoints
			}
			backend := tt.backend
			if backend == "" {
				backend = storage.BackendADX
			}
			_, err := parseRealtimeConfig(f, endpts, testMaxDiskUsage, backend)
			require.ErrorContains(t, err, tt.contains)
		})
	}
}

func TestParseStreamingBudget(t *testing.T) {
	endpoint, budget, err := parseStreamingBudget(" https://a.kusto.windows.net/?x=y = 42 ")
	require.NoError(t, err)
	require.Equal(t, "https://a.kusto.windows.net/?x=y", endpoint)
	require.Equal(t, 42, budget)
}

func TestNewRealtimeConfig_FromCLI(t *testing.T) {
	var got *realtimeConfig
	app := &cli.App{
		Flags: append([]cli.Flag{
			&cli.StringSliceFlag{Name: "metrics-kusto-endpoints"},
			&cli.StringSliceFlag{Name: "logs-kusto-endpoints"},
		}, realtimeCLIFlags()...),
		Action: func(ctx *cli.Context) error {
			var err error
			got, err = newRealtimeConfig(ctx,
				ctx.StringSlice("metrics-kusto-endpoints"),
				ctx.StringSlice("logs-kusto-endpoints"),
				testMaxDiskUsage, storage.BackendADX)
			return err
		},
	}

	err := app.Run([]string{"ingestor",
		"--metrics-kusto-endpoints", "Metrics=" + testMetricsEndpoint,
		"--logs-kusto-endpoints", "Logs=" + testLogsEndpoint,
		"--realtime-table", "Metrics.CpuUsage",
		"--realtime-table", "Logs.ApplicationErrors",
		"--realtime-streaming-budget", testMetricsEndpoint + "=100",
		"--realtime-streaming-budget", testLogsEndpoint + "=50",
		"--realtime-max-segment-age", "100ms",
		"--realtime-max-lag", "1m",
	})
	require.NoError(t, err)
	require.Len(t, got.Policy.RealtimeTables(), 2)
	require.Equal(t, 100*time.Millisecond, got.MaxSegmentAge)
	require.Equal(t, time.Minute, got.MaxLag)
	require.Equal(t, defaultRealtimeMaxBatchBytes, got.MaxBatchBytes)
	require.Equal(t, defaultQueuedReservedWorkersPercent, got.QueuedReservedWorkersPercent)
	require.Equal(t, map[string]int{testMetricsEndpoint: 100, testLogsEndpoint: 50}, got.StreamingBudgets)
}

func TestNewRealtimeConfig_DefaultsFromCLI(t *testing.T) {
	var got *realtimeConfig
	app := &cli.App{
		Flags: realtimeCLIFlags(),
		Action: func(ctx *cli.Context) error {
			var err error
			got, err = newRealtimeConfig(ctx, nil, nil, testMaxDiskUsage, storage.BackendADX)
			return err
		},
	}
	require.NoError(t, app.Run([]string{"ingestor"}))
	require.False(t, got.Policy.HasRealtime())
	require.Equal(t, defaultRealtimeMinSlots, got.MinSlots)
	require.Equal(t, defaultRealtimeMaxSegmentAge, got.MaxSegmentAge)
	require.Equal(t, defaultRealtimeMaxBatchLatency, got.MaxBatchLatency)
	require.Equal(t, defaultRealtimeMaxBatchBytes, got.MaxBatchBytes)
	require.Equal(t, defaultRealtimeMaxLag, got.MaxLag)
}

func TestNewRealtimeConfig_InvalidStorageEndpoint(t *testing.T) {
	app := &cli.App{
		Flags: realtimeCLIFlags(),
		Action: func(ctx *cli.Context) error {
			_, err := newRealtimeConfig(ctx, []string{"no-equals"}, nil, testMaxDiskUsage, storage.BackendADX)
			return err
		},
	}
	require.ErrorContains(t, app.Run([]string{"ingestor"}), "invalid endpoint")
}

func newTestRealtimeConfig(t *testing.T) *realtimeConfig {
	t.Helper()
	f := defaultRealtimeFlags()
	f.Tables = []string{"Metrics.CpuUsage"}
	f.StreamingBudgets = []string{testMetricsEndpoint + "=10"}
	endpoints := map[string]string{
		"Metrics":      testMetricsEndpoint,
		"OtherMetrics": testMetricsEndpoint + "/",
		"Logs":         testLogsEndpoint,
	}
	cfg, err := parseRealtimeConfig(f, endpoints, testMaxDiskUsage, storage.BackendADX)
	require.NoError(t, err)
	return cfg
}

func TestRealtimeConfig_NewStreamingSlots(t *testing.T) {
	cfg := newTestRealtimeConfig(t)
	slots := cfg.newStreamingSlots()
	require.Len(t, slots, 1)
	require.Equal(t, 10, slots[testMetricsEndpoint].Stats().Budget)

	disabled, err := parseRealtimeConfig(defaultRealtimeFlags(), testDatabaseEndpoints, testMaxDiskUsage, storage.BackendADX)
	require.NoError(t, err)
	require.Empty(t, disabled.newStreamingSlots())
}

func TestRealtimeConfig_UploadOpts(t *testing.T) {
	cfg := newTestRealtimeConfig(t)
	cfg.MaxLag = time.Minute
	slots := cfg.newStreamingSlots()

	opts := cfg.uploadOpts("Metrics", testMetricsEndpoint, slots)
	require.NotNil(t, opts)
	require.Same(t, slots[testMetricsEndpoint], opts.Slots)
	require.Equal(t, time.Minute, opts.MaxLag)

	// Databases without realtime tables do not stream.
	require.Nil(t, cfg.uploadOpts("OtherMetrics", testMetricsEndpoint, slots))
	require.Nil(t, cfg.uploadOpts("Logs", testLogsEndpoint, slots))
	// Endpoints are normalized when looking up slots.
	require.NotNil(t, cfg.uploadOpts("Metrics", "HTTPS://metrics.kusto.windows.net/", slots))
	// Without a slot pool for the endpoint, the database does not stream.
	require.Nil(t, cfg.uploadOpts("Metrics", testLogsEndpoint, slots))
}

func TestRealtimeConfig_ServiceOpts(t *testing.T) {
	cfg := newTestRealtimeConfig(t)
	opts := cfg.serviceOpts()
	require.NotNil(t, opts)
	require.Same(t, cfg.Policy, opts.Policy)
	require.Equal(t, defaultRealtimeMaxSegmentAge, opts.MaxSegmentAge)
	require.Equal(t, defaultRealtimeMaxBatchLatency, opts.MaxBatchLatency)
	require.Equal(t, defaultRealtimeMaxBatchBytes, opts.MaxBatchBytes)
	require.Equal(t, defaultRealtimeReservedDiskBytes, opts.ReservedDiskBytes)

	disabled, err := parseRealtimeConfig(defaultRealtimeFlags(), testDatabaseEndpoints, testMaxDiskUsage, storage.BackendADX)
	require.NoError(t, err)
	require.Nil(t, disabled.serviceOpts())
}

func TestPeerListenersUpdateSlots(t *testing.T) {
	cfg := newTestRealtimeConfig(t)
	slots := cfg.newStreamingSlots()
	listeners := peerListeners(slots)
	require.Len(t, listeners, 1)

	listeners[0](cluster.PeerInfo{Count: 4, Rank: 3})
	require.Equal(t, 2, slots[testMetricsEndpoint].Stats().Share)
}
