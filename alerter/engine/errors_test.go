package engine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnknownDBError_Basic(t *testing.T) {
	err := &UnknownDBError{
		DB:                 "cluster_state",
		AvailableDatabases: []string{"db1", "db2"},
	}

	errMsg := err.Error()
	require.Contains(t, errMsg, `no client for database "cluster_state"`)
	require.Contains(t, errMsg, "--kusto-endpoint")
	require.Contains(t, errMsg, "db1")
	require.Contains(t, errMsg, "db2")
}

func TestUnknownDBError_CaseInsensitiveMatch(t *testing.T) {
	err := &UnknownDBError{
		DB:                   "cluster_state",
		AvailableDatabases:   []string{"Cluster_State", "db2"},
		CaseInsensitiveMatch: "Cluster_State",
	}

	errMsg := err.Error()
	require.Contains(t, errMsg, `no client for database "cluster_state"`)
	require.Contains(t, errMsg, `did you mean "Cluster_State"?`)
	require.Contains(t, errMsg, "case-sensitive")
	require.Contains(t, errMsg, "--kusto-endpoint")
}

func TestUnknownDBError_NoDatabases(t *testing.T) {
	err := &UnknownDBError{
		DB:                 "cluster_state",
		AvailableDatabases: []string{},
	}

	errMsg := err.Error()
	require.Contains(t, errMsg, `no client for database "cluster_state"`)
	require.Contains(t, errMsg, "no databases configured via --kusto-endpoint")
}

func TestUnknownDBError_TruncateOver10(t *testing.T) {
	dbs := make([]string, 15)
	for i := 0; i < 15; i++ {
		dbs[i] = "db" + string(rune('a'+i))
	}

	err := &UnknownDBError{
		DB:                 "unknown",
		AvailableDatabases: dbs,
	}

	errMsg := err.Error()
	require.Contains(t, errMsg, `no client for database "unknown"`)
	require.Contains(t, errMsg, "--kusto-endpoint")

	// Should contain first 10 databases
	for i := 0; i < 10; i++ {
		require.Contains(t, errMsg, dbs[i])
	}

	// Should NOT contain databases 11-15
	for i := 10; i < 15; i++ {
		require.NotContains(t, errMsg, dbs[i])
	}

	// Should show "... and 5 more"
	require.Contains(t, errMsg, "... and 5 more")
}

func TestUnknownDBError_Exactly10Databases(t *testing.T) {
	dbs := make([]string, 10)
	for i := 0; i < 10; i++ {
		dbs[i] = "db" + string(rune('a'+i))
	}

	err := &UnknownDBError{
		DB:                 "unknown",
		AvailableDatabases: dbs,
	}

	errMsg := err.Error()

	// Should contain all 10 databases
	for i := 0; i < 10; i++ {
		require.Contains(t, errMsg, dbs[i])
	}

	// Should NOT have truncation message
	require.NotContains(t, errMsg, "... and")
	require.NotContains(t, errMsg, "more")
}

func TestUnknownDBError_FullMessage(t *testing.T) {
	err := &UnknownDBError{
		DB:                   "CLUSTER_STATE",
		AvailableDatabases:   []string{"cluster_state", "logs", "metrics"}, // Already sorted
		CaseInsensitiveMatch: "cluster_state",
	}

	errMsg := err.Error()
	// The message should be well-structured
	require.True(t, strings.HasPrefix(errMsg, `no client for database "CLUSTER_STATE"`))
	require.Contains(t, errMsg, `did you mean "cluster_state"? (database names are case-sensitive)`)
	require.Contains(t, errMsg, "configured databases via --kusto-endpoint: [cluster_state, logs, metrics]")
}
