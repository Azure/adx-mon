// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package config

import (
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"
)

var testRealtimeTables = []*RealtimeTable{{Database: "Metrics", Table: "CpuUsage"}}

func TestRealtime_ParseTOML(t *testing.T) {
	const data = `
max-disk-usage = 21474836480

[realtime]
max-segment-age-ms = 100
max-batch-latency-ms = 200
max-batch-bytes = 1048576
reserved-disk-bytes = 536870912
queued-reserved-workers-percent = 20

[[realtime.tables]]
database = "Metrics"
table = "CpuUsage"

[[realtime.tables]]
database = "Logs"
table = "ApplicationErrors"
`
	var c Config
	require.NoError(t, toml.Unmarshal([]byte(data), &c))
	require.NoError(t, c.Validate())

	r := c.Realtime
	require.NotNil(t, r)
	require.Equal(t, 100*time.Millisecond, r.MaxSegmentAge())
	require.Equal(t, 200*time.Millisecond, r.MaxBatchLatency())
	require.Equal(t, int64(1048576), r.MaxBatchBytes)
	require.Equal(t, int64(536870912), r.ReservedDiskBytes)
	require.Equal(t, 20, r.QueuedReservedWorkersPercent)

	p, err := r.Policy()
	require.NoError(t, err)
	require.Equal(t, ingestpolicy.PriorityRealtime, p.Priority("Metrics", "CpuUsage"))
	require.Equal(t, ingestpolicy.PriorityRealtime, p.Priority("Logs", "ApplicationErrors"))
	require.Equal(t, ingestpolicy.PriorityQueued, p.Priority("Metrics", "MemoryUsage"))
}

func TestRealtime_Defaults(t *testing.T) {
	c := Config{Realtime: &Realtime{}}
	require.NoError(t, c.Validate())

	r := c.Realtime
	require.Equal(t, DefaultRealtimeMaxSegmentAge, r.MaxSegmentAge())
	require.Equal(t, DefaultRealtimeMaxBatchLatency, r.MaxBatchLatency())
	require.Equal(t, DefaultRealtimeMaxBatchBytes, r.MaxBatchBytes)
	// No disk is reserved without realtime tables so queued capacity is unchanged.
	require.Zero(t, r.ReservedDiskBytes)
	require.Equal(t, DefaultRealtimeQueuedReservedWorkersPercent, r.QueuedReservedWorkersPercent)

	p, err := r.Policy()
	require.NoError(t, err)
	require.False(t, p.HasRealtime())
}

func TestRealtime_DefaultReservedDiskWithTables(t *testing.T) {
	c := Config{Realtime: &Realtime{Tables: testRealtimeTables}}
	require.NoError(t, c.Validate())
	require.Equal(t, DefaultRealtimeReservedDiskBytes, c.Realtime.ReservedDiskBytes)
}

func TestRealtime_NoTablesIgnoresDiskUsage(t *testing.T) {
	// A [realtime] section without tables must not fail on small disks or reduce queued capacity.
	c := Config{MaxDiskUsage: 1024, Realtime: &Realtime{ReservedDiskBytes: 4096}}
	require.NoError(t, c.Validate())
	require.Zero(t, c.Realtime.ReservedDiskBytes)
}

func TestRealtime_Unset(t *testing.T) {
	var c Config
	require.NoError(t, c.Validate())
	require.Nil(t, c.Realtime)

	p, err := c.Realtime.Policy()
	require.NoError(t, err)
	require.False(t, p.HasRealtime())
	require.Equal(t, ingestpolicy.PriorityQueued, p.Priority("Metrics", "CpuUsage"))
}

func TestRealtime_Validate(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		contains string
	}{
		{
			name:     "negative max segment age",
			config:   Config{Realtime: &Realtime{MaxSegmentAgeMs: -1}},
			contains: "realtime.max-segment-age-ms",
		},
		{
			name:     "negative max batch latency",
			config:   Config{Realtime: &Realtime{MaxBatchLatencyMs: -1}},
			contains: "realtime.max-batch-latency-ms",
		},
		{
			name:     "negative max batch bytes",
			config:   Config{Realtime: &Realtime{MaxBatchBytes: -1}},
			contains: "realtime.max-batch-bytes",
		},
		{
			name:     "negative reserved disk",
			config:   Config{Realtime: &Realtime{ReservedDiskBytes: -1}},
			contains: "realtime.reserved-disk-bytes must be greater than 0",
		},
		{
			name:     "reserved disk equals max disk usage",
			config:   Config{MaxDiskUsage: 1024, Realtime: &Realtime{ReservedDiskBytes: 1024, Tables: testRealtimeTables}},
			contains: "must be less than max-disk-usage (1024)",
		},
		{
			name:     "default reserved disk exceeds small max disk usage",
			config:   Config{MaxDiskUsage: 1024, Realtime: &Realtime{Tables: testRealtimeTables}},
			contains: "must be less than max-disk-usage (1024)",
		},
		{
			name:     "reserved disk exceeds default max disk usage",
			config:   Config{Realtime: &Realtime{ReservedDiskBytes: DefaultMaxDiskUsage, Tables: testRealtimeTables}},
			contains: "must be less than max-disk-usage",
		},
		{
			name:     "negative reserved workers",
			config:   Config{Realtime: &Realtime{QueuedReservedWorkersPercent: -1}},
			contains: "between 1 and 99",
		},
		{
			name:     "all workers reserved",
			config:   Config{Realtime: &Realtime{QueuedReservedWorkersPercent: 100}},
			contains: "between 1 and 99",
		},
		{
			name:     "nil table",
			config:   Config{Realtime: &Realtime{Tables: []*RealtimeTable{nil}}},
			contains: "realtime.tables[0] must not be empty",
		},
		{
			name:     "missing database",
			config:   Config{Realtime: &Realtime{Tables: []*RealtimeTable{{Table: "CpuUsage"}}}},
			contains: "realtime.tables[0].database must be set",
		},
		{
			name: "missing table",
			config: Config{Realtime: &Realtime{Tables: []*RealtimeTable{
				{Database: "Metrics", Table: "CpuUsage"},
				{Database: "Metrics"},
			}}},
			contains: "realtime.tables[1].table must be set",
		},
		{
			name: "duplicate table after normalization",
			config: Config{Realtime: &Realtime{Tables: []*RealtimeTable{
				{Database: "Metrics", Table: "CpuUsage"},
				{Database: "Metrics", Table: "Cpu_Usage"},
			}}},
			contains: `realtime.tables: duplicate realtime table "Metrics.CpuUsage"`,
		},
		{
			name: "table without valid characters",
			config: Config{Realtime: &Realtime{Tables: []*RealtimeTable{
				{Database: "Metrics", Table: "---"},
			}}},
			contains: "realtime.tables: invalid realtime table",
		},
		{
			name:     "clickhouse backend",
			config:   Config{StorageBackend: "clickhouse", Realtime: &Realtime{}},
			contains: `realtime is only supported with storage-backend "adx"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			require.Error(t, err)
			require.ErrorContains(t, err, tt.contains)
		})
	}
}

func TestRealtime_ReservedWorkersBounds(t *testing.T) {
	for _, pct := range []int{1, 50, 99} {
		c := Config{Realtime: &Realtime{QueuedReservedWorkersPercent: pct}}
		require.NoError(t, c.Validate(), "percent %d", pct)
		require.Equal(t, pct, c.Realtime.QueuedReservedWorkersPercent)
	}
}

func TestRealtime_RoundTripTOML(t *testing.T) {
	in := Config{Realtime: &Realtime{
		MaxSegmentAgeMs: 300,
		Tables:          []*RealtimeTable{{Database: "Metrics", Table: "CpuUsage"}},
	}}
	b, err := toml.Marshal(in)
	require.NoError(t, err)

	var out Config
	require.NoError(t, toml.Unmarshal(b, &out))
	require.Equal(t, in.Realtime, out.Realtime)
}
