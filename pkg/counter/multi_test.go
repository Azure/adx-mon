package counter

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMultiEstimator(t *testing.T) {
	est := NewMultiEstimator()
	require.Equal(t, uint64(0), est.Count("foo"))

	est.Add("foo", 1)
	require.Equal(t, uint64(1), est.Count("foo"))

	est.Add("bar", 1)
	require.Equal(t, uint64(1), est.Count("bar"))

	est.Add("foo", 2)
	require.Equal(t, uint64(2), est.Count("foo"))

	est.Roll()
	require.Equal(t, uint64(2), est.Count("foo"))

	est.Roll()
	require.Equal(t, uint64(0), est.Count("foo"))

	est.Reset()
	require.Equal(t, uint64(0), est.Count("foo"))
	require.Equal(t, uint64(0), est.Count("bar"))
	require.Equal(t, uint64(0), est.Count("baz"))
}

func TestMultiEstimator_HighCount(t *testing.T) {
	samples := 1000000
	est := NewMultiEstimator()
	require.Equal(t, uint64(0), est.Count("foo"))

	for i := 0; i < samples; i++ {
		u := rand.Uint64()
		est.Add("foo", u)
	}
	require.True(t, float64(samples-int(est.Count("foo")))/float64(samples) < 0.1)
	est.Roll()
	require.True(t, float64(samples-int(est.Count("foo")))/float64(samples) < 0.1)
	est.Roll()
	require.Equal(t, uint64(0), est.Count("foo"))
}
