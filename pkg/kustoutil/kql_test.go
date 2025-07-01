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
let _endTime=datetime(2024-01-01T01:00:00Z);
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
let _endTime=datetime(2024-01-01T01:00:00Z);
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
let _endTime=datetime(2024-01-01T01:00:00Z);
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
