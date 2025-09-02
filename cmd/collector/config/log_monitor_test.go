package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLogMonitorBuildSet(t *testing.T) {
	lm := &LogMonitor{Pairs: []string{"db1:tbl1", "db2:tbl2", "bad", "db3:"}}
	lm.BuildSet()
	require.True(t, lm.IsMonitored("db1", "tbl1"), "expected db1:tbl1 to be monitored")
	require.True(t, lm.IsMonitored("db2", "tbl2"), "expected db2:tbl2 to be monitored")
	require.False(t, lm.IsMonitored("db3", ""), "db3: (empty table) should not be monitored")
	require.False(t, lm.IsMonitored("bad", ""), "invalid pair 'bad' should not be monitored")
}

func TestLogMonitorEmpty(t *testing.T) {
	var lm *LogMonitor
	require.False(t, lm.IsMonitored("a", "b"), "expected false for nil LogMonitor receiver")
}
