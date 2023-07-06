package counter

import (
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
