// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package ingestpolicy

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPriorityString(t *testing.T) {
	require.Equal(t, "queued", PriorityQueued.String())
	require.Equal(t, "realtime", PriorityRealtime.String())
	require.Equal(t, "unknown(7)", Priority(7).String())
	require.Equal(t, 2, NumPriorities)
}

func TestNilAndEmptyPolicy(t *testing.T) {
	var nilPolicy *Policy
	require.Equal(t, PriorityQueued, nilPolicy.Priority("Metrics", "CpuUsage"))
	require.Equal(t, PriorityQueued, nilPolicy.PriorityBytes([]byte("Metrics"), []byte("CpuUsage")))
	require.False(t, nilPolicy.HasRealtime())
	require.Nil(t, nilPolicy.RealtimeTables())
	require.Nil(t, nilPolicy.RealtimeDatabases())

	var zero Policy
	require.Equal(t, PriorityQueued, zero.Priority("Metrics", "CpuUsage"))
	require.False(t, zero.HasRealtime())

	empty, err := New(nil)
	require.NoError(t, err)
	require.Equal(t, PriorityQueued, empty.Priority("Metrics", "CpuUsage"))
	require.False(t, empty.HasRealtime())
	require.Empty(t, empty.RealtimeTables())
}

func TestPolicyPriority(t *testing.T) {
	p, err := New([]Table{
		{Database: "Metrics", Table: "CpuUsage"},
		{Database: "Logs", Table: "ApplicationErrors"},
	})
	require.NoError(t, err)
	require.True(t, p.HasRealtime())

	tests := []struct {
		db, table string
		want      Priority
	}{
		{"Metrics", "CpuUsage", PriorityRealtime},
		{"Logs", "ApplicationErrors", PriorityRealtime},
		{"Metrics", "MemoryUsage", PriorityQueued},
		{"Logs", "CpuUsage", PriorityQueued},
		{"Other", "CpuUsage", PriorityQueued},
		// Names are case sensitive, matching WAL keys and ADX identifiers.
		{"metrics", "CpuUsage", PriorityQueued},
		{"Metrics", "cpuusage", PriorityQueued},
		{"", "", PriorityQueued},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, p.Priority(tt.db, tt.table), "%s.%s", tt.db, tt.table)
		require.Equal(t, tt.want, p.PriorityBytes([]byte(tt.db), []byte(tt.table)), "%s.%s", tt.db, tt.table)
	}
}

func TestPolicyNormalizesNames(t *testing.T) {
	p, err := New([]Table{{Database: "My-Metrics", Table: "Cpu_Usage"}})
	require.NoError(t, err)
	require.Equal(t, PriorityRealtime, p.Priority("MyMetrics", "CpuUsage"))
	require.Equal(t, PriorityQueued, p.Priority("My-Metrics", "Cpu_Usage"))
	require.Equal(t, []Table{{Database: "MyMetrics", Table: "CpuUsage"}}, p.RealtimeTables())
}

func TestPolicyRejectsInvalid(t *testing.T) {
	_, err := New([]Table{{Database: "Metrics", Table: "---"}})
	require.ErrorContains(t, err, "invalid realtime table")

	_, err = New([]Table{{Database: "", Table: "CpuUsage"}})
	require.ErrorContains(t, err, "invalid realtime table")

	_, err = New([]Table{
		{Database: "Metrics", Table: "CpuUsage"},
		{Database: "Metrics", Table: "Cpu_Usage"},
	})
	require.ErrorContains(t, err, `duplicate realtime table "Metrics.CpuUsage"`)

	// All errors are reported.
	_, err = New([]Table{
		{Database: "", Table: "A"},
		{Database: "", Table: "B"},
	})
	require.ErrorContains(t, err, `".A"`)
	require.ErrorContains(t, err, `".B"`)
}

func TestPolicyListsSorted(t *testing.T) {
	p, err := New([]Table{
		{Database: "Metrics", Table: "B"},
		{Database: "Logs", Table: "Z"},
		{Database: "Metrics", Table: "A"},
	})
	require.NoError(t, err)
	require.Equal(t, []Table{
		{Database: "Logs", Table: "Z"},
		{Database: "Metrics", Table: "A"},
		{Database: "Metrics", Table: "B"},
	}, p.RealtimeTables())
	require.Equal(t, []string{"Logs", "Metrics"}, p.RealtimeDatabases())
}

func TestPolicyConcurrentReads(t *testing.T) {
	p, err := New([]Table{{Database: "Metrics", Table: "CpuUsage"}})
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				require.Equal(t, PriorityRealtime, p.Priority("Metrics", "CpuUsage"))
				require.Equal(t, PriorityQueued, p.Priority("Metrics", "Other"))
			}
		}()
	}
	wg.Wait()
}

func TestPriorityLookupsDoNotAllocate(t *testing.T) {
	p, err := New([]Table{{Database: "Metrics", Table: "CpuUsage"}})
	require.NoError(t, err)
	db, table := []byte("Metrics"), []byte("CpuUsage")

	allocs := testing.AllocsPerRun(100, func() {
		_ = p.Priority("Metrics", "CpuUsage")
		_ = p.Priority("Metrics", "Other")
		_ = p.PriorityBytes(db, table)
	})
	require.Zero(t, allocs)
}

func BenchmarkPriority(b *testing.B) {
	tables := make([]Table, 0, 100)
	for i := 0; i < 100; i++ {
		tables = append(tables, Table{Database: "Metrics", Table: "Table" + string(rune('A'+i%26)) + string(rune('a'+i/26))})
	}
	p, err := New(tables)
	require.NoError(b, err)

	b.Run("hit", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = p.Priority("Metrics", "TableAa")
		}
	})
	b.Run("miss", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = p.Priority("Metrics", "NotRealtime")
		}
	})
	b.Run("bytes", func(b *testing.B) {
		db, table := []byte("Metrics"), []byte("TableAa")
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = p.PriorityBytes(db, table)
		}
	})
	b.Run("empty", func(b *testing.B) {
		var empty *Policy
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = empty.Priority("Metrics", "TableAa")
		}
	})
}
