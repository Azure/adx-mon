package sourceparse

import (
	"testing"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/require"
)

func TestDockerParse(t *testing.T) {
	t.Run("Single full log", func(t *testing.T) {
		parser := NewDockerParser()
		log := types.NewLog()
		message, isPartial, err := parser.Parse(`{"log":"log1\n","stream":"stdout","time":"2021-07-01T00:00:00.000000000Z"}`, log)
		require.NoError(t, err)
		require.False(t, isPartial)
		require.Equal(t, uint64(1625097600000000000), log.GetTimestamp())
		require.Equal(t, "stdout", types.StringOrEmpty(log.GetBodyValue("stream")))
		require.Equal(t, "log1", message)
	})

	t.Run("Invalid formatted log", func(t *testing.T) {
		parser := NewDockerParser()
		log := types.NewLog()
		_, _, err := parser.Parse(`{"log":"log1\n","stream":"stdout","time":"2021-07-01T00:00:00.000000000Z`, log)
		require.Error(t, err)
	})

	t.Run("Single partial log", func(t *testing.T) {
		parser := NewDockerParser()
		log := types.NewLog()
		// No newline at the end of the "log" field
		_, isPartial, err := parser.Parse(`{"log":"log1","stream":"stdout","time":"2021-07-01T00:00:00.000000000Z"}`, log)
		require.NoError(t, err)
		require.True(t, isPartial)
	})

	t.Run("Partials combined", func(t *testing.T) {
		parser := NewDockerParser()
		log := types.NewLog()
		_, isPartial, err := parser.Parse(`{"log":"log1 ","stream":"stdout","time":"2021-07-01T00:00:00.000000000Z"}`, log)
		require.NoError(t, err)
		require.True(t, isPartial)

		_, isPartial, err = parser.Parse(`{"log":"log2 ","stream":"stdout","time":"2021-07-01T00:00:00.000000000Z"}`, log)
		require.NoError(t, err)
		require.True(t, isPartial)

		message, isPartial, err := parser.Parse(`{"log":"log3\n","stream":"stdout","time":"2021-07-01T00:00:00.000000000Z"}`, log)
		require.NoError(t, err)
		require.False(t, isPartial)
		require.Equal(t, uint64(1625097600000000000), log.GetTimestamp())
		require.Equal(t, "stdout", types.StringOrEmpty(log.GetBodyValue("stream")))
		require.Equal(t, "log1 log2 log3", message)
	})

	t.Run("Partials combined per stream", func(t *testing.T) {
		parser := NewDockerParser()
		log := types.NewLog()
		_, isPartial, err := parser.Parse(`{"log":"stdoutlog1 ","stream":"stdout","time":"2021-07-01T00:00:00.000000000Z"}`, log)
		require.NoError(t, err)
		require.True(t, isPartial)

		_, isPartial, err = parser.Parse(`{"log":"stderrlog1 ","stream":"stderr","time":"2021-07-01T00:00:00.000000000Z"}`, log)
		require.NoError(t, err)
		require.True(t, isPartial)

		log = types.NewLog()
		message, isPartial, err := parser.Parse(`{"log":"stdoutlog2\n","stream":"stdout","time":"2021-07-01T00:00:00.000000000Z"}`, log)
		require.NoError(t, err)
		require.False(t, isPartial)
		require.Equal(t, uint64(1625097600000000000), log.GetTimestamp())
		require.Equal(t, "stdout", types.StringOrEmpty(log.GetBodyValue("stream")))
		require.Equal(t, "stdoutlog1 stdoutlog2", message)

		log = types.NewLog()
		message, isPartial, err = parser.Parse(`{"log":"stderrlog2\n","stream":"stderr","time":"2021-07-01T00:00:00.000000000Z"}`, log)
		require.NoError(t, err)
		require.False(t, isPartial)
		require.Equal(t, uint64(1625097600000000000), log.GetTimestamp())
		require.Equal(t, "stderr", types.StringOrEmpty(log.GetBodyValue("stream")))
		require.Equal(t, "stderrlog1 stderrlog2", message)
	})
}

func BenchmarkDockerParser(b *testing.B) {
	parser := NewDockerParser()
	log := types.NewLog()

	for i := 0; i < b.N; i++ {
		parser.Parse(`{"log":"log1\n","stream":"stdout","time":"2021-07-01T00:00:00.000000000Z"}`, log)
	}
}
