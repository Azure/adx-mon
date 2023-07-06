package counter

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEstimator(t *testing.T) {
	est := NewEstimator()
	est.Add(1)
	est.Add(2)
	require.Equal(t, uint64(2), est.Count())

	est.Reset()
	require.Equal(t, uint64(0), est.Count())
}
