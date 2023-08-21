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

func BenchmarkEstimator_Add(b *testing.B) {
	est := NewEstimator()
	for i := 0; i < b.N; i++ {
		est.Add(uint64(i))
	}
}
