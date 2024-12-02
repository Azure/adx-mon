package kustainer_test

import (
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/stretchr/testify/require"
)

func TestIngestionBatchingPolicyRendering(t *testing.T) {
	tests := []struct {
		Duration time.Duration
		Expect   string
	}{
		{
			Duration: 32 * time.Second,
			Expect:   "00:00:32",
		},
		{
			Duration: 3 * time.Minute,
			Expect:   "00:03:00",
		},
		{
			Duration: 10 * time.Hour,
			Expect:   "10:00:00",
		},
	}
	for _, tt := range tests {
		t.Run(tt.Duration.String(), func(t *testing.T) {
			p := kustainer.IngestionBatchingPolicy{
				MaximumBatchingTimeSpan: 30 * time.Second,
			}
			d, err := p.MarshalJSON()
			require.NoError(t, err)
			require.JSONEq(t, `{"MaximumBatchingTimeSpan":"00:00:30"}`, string(d))
		})
	}
}
