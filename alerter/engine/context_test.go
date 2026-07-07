package engine

import (
	"testing"
	"time"

	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/stretchr/testify/require"
)

func TestNewQueryContext_MgmtQuery(t *testing.T) {
	r := &rules.Rule{IsMgmtQuery: true, Query: "fake query unchanged"}
	qc, err := NewQueryContext(r, time.Now(), "region")
	require.NoError(t, err)
	require.Equal(t, "fake query unchanged", qc.Query)
}

func TestNewQueryContext_QueryWrapped(t *testing.T) {

	// write subtests for each of the following cases: "Foo | limit 1", "   Foo | limit 1"
	for _, tt := range []struct {
		query, exp string
	}{
		{
			query: "Foo | limit 1",
			exp:   "\nlet _startTime = datetime(2023-04-09T23:00:00Z);\nlet _endTime = datetime(2023-04-10T00:00:00Z);\nlet _region = \"region\";\nFoo | limit 1\n",
		},
		{
			query: "    Foo | limit 1",
			exp:   "\nlet _startTime = datetime(2023-04-09T23:00:00Z);\nlet _endTime = datetime(2023-04-10T00:00:00Z);\nlet _region = \"region\";\nFoo | limit 1\n",
		},
	} {
		r := &rules.Rule{Query: tt.query, Interval: time.Hour}
		qc, err := NewQueryContext(r, time.Date(2023, 04, 10, 0, 0, 0, 0, time.UTC), "region")
		require.NoError(t, err)
		require.Equal(t, tt.exp, qc.Query)
	}
}

func TestQueryContext_ConstructsWrappedQuery(t *testing.T) {
	r := &rules.Rule{Query: "Foo | where Timestamp between (_startTime .. _endTime)", Interval: time.Hour}
	qc, err := NewQueryContext(r, time.Date(2023, 04, 10, 0, 0, 0, 0, time.UTC), "region")
	require.NoError(t, err)

	require.Len(t, qc.Options, 1)
	require.Equal(t, "\nlet _startTime = _adxmonStartTime;\nlet _endTime = _adxmonEndTime;\nlet _region = _adxmonRegion;\nFoo | where Timestamp between (_startTime .. _endTime)\n", qc.Stmt.String())
}

func TestQueryContext_ConstructsMgmtQuery(t *testing.T) {
	r := &rules.Rule{IsMgmtQuery: true, Query: ".show tables"}
	qc, err := NewQueryContext(r, time.Now(), "region")
	require.NoError(t, err)

	require.Empty(t, qc.Options)
	require.Equal(t, ".show tables", qc.Stmt.String())
}
