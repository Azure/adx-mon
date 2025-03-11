package sourceparse

import (
	"testing"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/require"
)

func TestCriParse(t *testing.T) {
	t.Run("Single full log", func(t *testing.T) {
		parser := NewCriParser()
		log := types.NewLog()
		message, isPartial, err := parser.Parse("2021-07-01T00:00:00.000000000Z stdout F log1", log)
		require.NoError(t, err)
		require.False(t, isPartial)
		require.Equal(t, uint64(1625097600000000000), log.GetTimestamp())
		require.Equal(t, "stdout", types.StringOrEmpty(log.GetBodyValue("stream")))
		require.Equal(t, "log1", message)
	})

	t.Run("Single empty log", func(t *testing.T) {
		parser := NewCriParser()
		log := types.NewLog()
		message, isPartial, err := parser.Parse("2021-07-01T00:00:00.000000000Z stdout F ", log)
		require.NoError(t, err)
		require.False(t, isPartial)
		require.Equal(t, uint64(1625097600000000000), log.GetTimestamp())
		require.Equal(t, "stdout", types.StringOrEmpty(log.GetBodyValue("stream")))
		require.Equal(t, "", message)
	})

	t.Run("Single partial log", func(t *testing.T) {
		parser := NewCriParser()
		log := types.NewLog()
		_, isPartial, err := parser.Parse("2021-07-01T00:00:00.000000000Z stdout P log1", log)
		require.NoError(t, err)
		require.True(t, isPartial)
	})

	t.Run("Invalid logs", func(t *testing.T) {
		invalidLogs := []string{
			"2021-07-01T00:00:00.000000000Z stdout F", // no message
			"2021-07-01T00:00:00.000000000Z stdout ",  // no tag
			"2021-07-01T00:00:00.000000000Z stdout",   // no tag
			"2021-07-01T00:00:00.000000000Z ",         // no stream
			"2021-07-01T00:00:00.000000000Z",          // no stream
			"",                                        // empty
			"2021-07-01T stdout F logmsg",             // malformed timestamp
		}

		for _, line := range invalidLogs {
			parser := NewCriParser()
			log := types.NewLog()
			_, _, err := parser.Parse(line, log)
			require.Error(t, err)
		}
	})

	t.Run("Partials combined", func(t *testing.T) {
		parser := NewCriParser()
		log := types.NewLog()
		_, isPartial, err := parser.Parse("2021-07-01T00:00:00.000000000Z stdout P log1 ", log)
		require.NoError(t, err)
		require.True(t, isPartial)

		_, isPartial, err = parser.Parse("2021-07-01T00:00:00.000000000Z stdout P log2 ", log)
		require.NoError(t, err)
		require.True(t, isPartial)

		message, isPartial, err := parser.Parse("2021-07-01T00:00:00.000000000Z stdout F log3", log)
		require.NoError(t, err)
		require.False(t, isPartial)
		require.Equal(t, uint64(1625097600000000000), log.GetTimestamp())
		require.Equal(t, "stdout", types.StringOrEmpty(log.GetBodyValue("stream")))
		require.Equal(t, "log1 log2 log3", message)
	})

	t.Run("Partials combined per stream", func(t *testing.T) {
		parser := NewCriParser()
		log := types.NewLog()
		_, isPartial, err := parser.Parse("2021-07-01T00:00:00.000000000Z stdout P stdoutlog1 message ", log)
		require.NoError(t, err)
		require.True(t, isPartial)

		_, isPartial, err = parser.Parse("2021-07-01T00:00:00.000000000Z stderr P stderrlog1 ", log)
		require.NoError(t, err)
		require.True(t, isPartial)

		message, isPartial, err := parser.Parse("2021-07-01T00:00:00.000000000Z stdout F stdoutlog2", log)
		require.NoError(t, err)
		require.False(t, isPartial)
		require.Equal(t, uint64(1625097600000000000), log.GetTimestamp())
		require.Equal(t, "stdout", types.StringOrEmpty(log.GetBodyValue("stream")))
		require.Equal(t, "stdoutlog1 message stdoutlog2", message)

		log = types.NewLog()
		message, isPartial, err = parser.Parse("2021-07-01T00:00:00.000000000Z stderr F stderrlog2", log)
		require.NoError(t, err)
		require.False(t, isPartial)
		require.Equal(t, uint64(1625097600000000000), log.GetTimestamp())
		require.Equal(t, "stderr", types.StringOrEmpty(log.GetBodyValue("stream")))
		require.Equal(t, "stderrlog1 stderrlog2", message)
	})
}

func BenchmarkCriParse(b *testing.B) {
	parser := NewCriParser()
	log := types.NewLog()

	for i := 0; i < b.N; i++ {
		parser.Parse("2021-07-01T00:00:00.000000000Z stdout F log1", log)
	}
}
