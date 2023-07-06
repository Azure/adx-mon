package counter

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRollingEstimator_Roll(t *testing.T) {
	r := NewRollingEstimator()
	require.Equal(t, uint64(0), r.Count())

	r.Add(1)
	r.Add(2)
	r.Add(3)

	require.Equal(t, uint64(3), r.Count())

	r.Roll()

	require.Equal(t, uint64(3), r.Count())

	r.Add(1)
	r.Add(2)
	require.Equal(t, uint64(3), r.Count())

	r.Roll()

	require.Equal(t, uint64(2), r.Count())

	r.Roll()

	require.Equal(t, uint64(0), r.Count())

	r.Reset()
	require.Equal(t, uint64(0), r.Count())
}
