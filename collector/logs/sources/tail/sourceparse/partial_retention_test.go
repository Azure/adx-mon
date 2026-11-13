package sourceparse

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/require"
)

func partialBytes(p LogTypeParser) int {
	switch p := p.(type) {
	case *CriParser:
		return p.streamPartials.RetainedBytes()
	case *DockerParser:
		return p.streamPartials.RetainedBytes()
	}
	return 0
}

func partialLine(kind, stream, message string, partial bool) string {
	if kind == "cri" {
		tag := "F"
		if partial {
			tag = "P"
		}
		return "2021-07-01T00:00:00Z " + stream + " " + tag + " " + message
	}
	if !partial {
		message += "\n"
	}
	b, _ := json.Marshal(DockerLog{Log: message, Stream: stream, Time: "2021-07-01T00:00:00Z"})
	return string(b)
}

func TestPartialParsersBoundAndExpire(t *testing.T) {
	for _, kind := range []string{"cri", "docker"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var p LogTypeParser = NewCriParser()
				if kind == "docker" {
					p = NewDockerParser()
				}
				log := types.NewLog()
				for range 80 {
					_, pending, err := p.Parse(partialLine(kind, "stdout", strings.Repeat("x", 32<<10), true), log)
					require.NoError(t, err)
					require.True(t, pending)
				}
				require.LessOrEqual(t, partialBytes(p), 1<<20, "unterminated messages must have a size bound")
				_, pending, err := p.Parse(partialLine(kind, "stdout", "discarded final", false), log)
				require.NoError(t, err)
				require.True(t, pending, "drop an oversized assembly through its final fragment")
				message, pending, err := p.Parse(partialLine(kind, "stdout", "recovered", false), log)
				require.NoError(t, err)
				require.False(t, pending)
				require.Equal(t, "recovered", message)
				_, _, err = p.Parse(partialLine(kind, "stderr", "abandoned", true), log)
				require.NoError(t, err)
				time.Sleep(6 * time.Minute)
				if expirer, ok := p.(interface{ ExpirePartials(time.Time) }); ok {
					expirer.ExpirePartials(time.Now())
				}
				require.Zero(t, partialBytes(p), "idle expiry releases abandoned bytes without another fragment")
				_, _, err = p.Parse(partialLine(kind, "stderr", "new ", true), log)
				require.NoError(t, err)
				message, pending, err = p.Parse(partialLine(kind, "stderr", "message", false), log)
				require.NoError(t, err)
				require.False(t, pending)
				require.Equal(t, "new message", message)
			})
		})
	}
}

func TestKubernetesParserExpiresUnderlyingPartials(t *testing.T) {
	for _, kind := range []string{"cri", "docker"} {
		p := NewKubernetesParser()
		_, pending, err := p.Parse(partialLine(kind, "stdout", "abandoned", true), types.NewLog())
		require.NoError(t, err)
		require.True(t, pending)
		require.Positive(t, partialBytes(p.parser))
		p.ExpirePartials(time.Now().Add(6 * time.Minute))
		require.Zero(t, partialBytes(p.parser))
	}
}
