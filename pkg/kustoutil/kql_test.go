package kustoutil

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplySubstitutions(t *testing.T) {
	tests := []struct {
		Name          string
		RuleBody      string
		ClusterLabels map[string]string
		Want          string
	}{
		{
			Name: "timestamps",
			RuleBody: `T
| where Timestamp between( _startTime .. _endTime )`,
			Want: `let _startTime=datetime(2024-01-01T00:00:00Z);
let _endTime=datetime(2024-01-01T00:59:59.9999999Z);
T
| where Timestamp between( _startTime .. _endTime )`,
		},
		{
			Name: "region",
			RuleBody: `T
| where Timestamp between( _startTime .. _endTime )
| where Region == _region`,
			ClusterLabels: map[string]string{
				"_region": "eastus",
			},
			Want: `let _startTime=datetime(2024-01-01T00:00:00Z);
let _endTime=datetime(2024-01-01T00:59:59.9999999Z);
let _region="eastus";
T
| where Timestamp between( _startTime .. _endTime )
| where Region == _region`,
		},
		{
			Name: "region and environment",
			RuleBody: `T
| where Timestamp between( _startTime .. _endTime )
| where Region == _region
| where Environment != _environment`,
			ClusterLabels: map[string]string{
				"_region":      "eastus",
				"_environment": "production",
			},
			Want: `let _startTime=datetime(2024-01-01T00:00:00Z);
let _endTime=datetime(2024-01-01T00:59:59.9999999Z);
let _environment="production";
let _region="eastus";
T
| where Timestamp between( _startTime .. _endTime )
| where Region == _region
| where Environment != _environment`,
		},
	}
	startTime := "2024-01-01T00:00:00Z"
	endTime := "2024-01-01T01:00:00Z"
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			have := ApplySubstitutions(tt.RuleBody, startTime, endTime, tt.ClusterLabels)
			require.Equal(t, tt.Want, have)
		})
	}
}

func TestSubtractOneTick(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic RFC3339Nano time",
			input:    "2024-01-01T01:00:00.000000000Z",
			expected: "2024-01-01T00:59:59.9999999Z",
		},
		{
			name:     "time with nanoseconds",
			input:    "2024-01-01T01:00:00.123456789Z",
			expected: "2024-01-01T01:00:00.123456689Z",
		},
		{
			name:     "time at minute boundary",
			input:    "2024-01-01T01:00:00Z",
			expected: "2024-01-01T00:59:59.9999999Z",
		},
		{
			name:     "time with microseconds",
			input:    "2024-01-01T01:00:00.123456Z",
			expected: "2024-01-01T01:00:00.1234559Z",
		},
		{
			name:     "invalid time string returns original",
			input:    "invalid-time",
			expected: "invalid-time",
		},
		{
			name:     "empty string returns original",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := subtractOneTick(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestApplySubstitutionsBetweenSyntax(t *testing.T) {
	t.Run("endTime is adjusted for between syntax", func(t *testing.T) {
		ruleBody := `SomeTable
| where PreciseTimeStamp between (_startTime .. _endTime)
| summarize count() by bin(PreciseTimeStamp, 1h)`

		startTime := "2024-01-01T10:00:00.000000000Z"
		endTime := "2024-01-01T11:00:00.000000000Z"

		result := ApplySubstitutions(ruleBody, startTime, endTime, nil)

		expected := `let _startTime=datetime(2024-01-01T10:00:00.000000000Z);
let _endTime=datetime(2024-01-01T10:59:59.9999999Z);
SomeTable
| where PreciseTimeStamp between (_startTime .. _endTime)
| summarize count() by bin(PreciseTimeStamp, 1h)`

		require.Equal(t, expected, result)
	})

	t.Run("startTime remains unchanged", func(t *testing.T) {
		ruleBody := `SomeTable | where PreciseTimeStamp between (_startTime .. _endTime)`
		startTime := "2024-01-01T10:00:00.000000000Z"
		endTime := "2024-01-01T11:00:00.000000000Z"

		result := ApplySubstitutions(ruleBody, startTime, endTime, nil)

		// Verify startTime is unchanged
		require.Contains(t, result, "let _startTime=datetime(2024-01-01T10:00:00.000000000Z);")
		// Verify endTime is adjusted
		require.Contains(t, result, "let _endTime=datetime(2024-01-01T10:59:59.9999999Z);")
	})

	t.Run("works with cluster labels", func(t *testing.T) {
		ruleBody := `SomeTable
| where PreciseTimeStamp between (_startTime .. _endTime)
| where Region == _region`

		startTime := "2024-01-01T10:00:00Z"
		endTime := "2024-01-01T11:00:00Z"
		clusterLabels := map[string]string{"region": "eastus"}

		result := ApplySubstitutions(ruleBody, startTime, endTime, clusterLabels)

		require.Contains(t, result, "let _startTime=datetime(2024-01-01T10:00:00Z);")
		require.Contains(t, result, "let _endTime=datetime(2024-01-01T10:59:59.9999999Z);")
		require.Contains(t, result, `let _region="eastus";`)
	})
}
