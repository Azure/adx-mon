package sourceparse

import (
	"testing"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/require"
)

func TestKubernetesParser(t *testing.T) {
	t.Run("Docker log", func(t *testing.T) {
		parser := NewKubernetesParser()
		log := types.NewLog()
		isPartial, err := parser.Parse(`{"log":"log1\n","stream":"stdout","time":"2021-07-01T00:00:00.000000000Z"}`, log)
		require.NoError(t, err)
		require.False(t, isPartial)
		require.Equal(t, uint64(1625097600000000000), log.Timestamp)
		require.Equal(t, "stdout", log.Body["stream"])
		require.Equal(t, "log1", log.Body[types.BodyKeyMessage])

		log = types.NewLog()
		isPartial, err = parser.Parse(`{"log":"log2\n","stream":"stdout","time":"2021-07-01T00:00:00.000000000Z"}`, log)
		require.NoError(t, err)
		require.False(t, isPartial)
		require.Equal(t, uint64(1625097600000000000), log.Timestamp)
		require.Equal(t, "stdout", log.Body["stream"])
		require.Equal(t, "log2", log.Body[types.BodyKeyMessage])
	})

	t.Run("Cri log", func(t *testing.T) {
		parser := NewKubernetesParser()
		log := types.NewLog()
		isPartial, err := parser.Parse("2021-07-01T00:00:00.000000000Z stdout F log1", log)
		require.NoError(t, err)
		require.False(t, isPartial)
		require.Equal(t, uint64(1625097600000000000), log.Timestamp)
		require.Equal(t, "stdout", log.Body["stream"])
		require.Equal(t, "log1", log.Body[types.BodyKeyMessage])

		log = types.NewLog()
		isPartial, err = parser.Parse("2021-07-01T00:00:00.000000000Z stdout F log2", log)
		require.NoError(t, err)
		require.False(t, isPartial)
		require.Equal(t, uint64(1625097600000000000), log.Timestamp)
		require.Equal(t, "stdout", log.Body["stream"])
		require.Equal(t, "log2", log.Body[types.BodyKeyMessage])
	})
}

func BenchmarkKubernetesParse(b *testing.B) {
	parser := NewKubernetesParser()
	log := types.NewLog()

	for i := 0; i < b.N; i++ {
		parser.Parse("2021-07-01T00:00:00.000000000Z stdout F log1", log)
	}
}
