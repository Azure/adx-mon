package logger_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/stretchr/testify/require"
)

func BenchmarkInfof(b *testing.B) {
	destination := "MDM://AKSCONTROLPLANE"
	title := "[AzureCloud/prod][centralus] ingress containerservice/containerservice-ingress success rate below 99"
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		logger.Infof("Received alert notification: [%s] %s", destination, title)
	}
}

func BenchmarkInfo(b *testing.B) {
	destination := "MDM://AKSCONTROLPLANE"
	title := "[AzureCloud/prod][centralus] ingress containerservice/containerservice-ingress success rate below 99"
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		logger.Info("Received alert notification:", "destination", destination, "title", title)
	}
}

func TestSetLevel(t *testing.T) {
	defer logger.SetLevel(slog.LevelInfo) // reset

	// Initial state - INFO
	require.False(t, logger.IsDebug())
	require.True(t, logger.IsInfo())
	require.True(t, logger.IsWarn())

	logger.SetLevel(slog.LevelDebug)
	require.True(t, logger.IsDebug())
	require.True(t, logger.IsInfo())
	require.True(t, logger.IsWarn())

	logger.SetLevel(slog.LevelWarn)
	require.False(t, logger.IsDebug())
	require.False(t, logger.IsInfo())
	require.True(t, logger.IsWarn())

	logger.SetLevel(slog.LevelError)
	require.False(t, logger.IsDebug())
	require.False(t, logger.IsInfo())
	require.False(t, logger.IsWarn())
}

func TestLoggerWritesJSONToStderr(t *testing.T) {
	if os.Getenv("ADX_MON_LOGGER_SUBPROCESS") == "1" {
		logger.Info("operational message")
		os.Exit(0)
	}

	command := exec.Command(os.Args[0], "-test.run=^TestLoggerWritesJSONToStderr$")
	command.Env = append(os.Environ(), "ADX_MON_LOGGER_SUBPROCESS=1", "LOG_LEVEL=INVALID")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	require.NoError(t, command.Run())
	require.Empty(t, stdout.String())

	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	require.Len(t, lines, 2)
	messages := make([]string, 0, len(lines))
	for _, line := range lines {
		var record map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &record), line)
		messages = append(messages, record["msg"].(string))
	}
	require.Equal(t, []string{"Unknown log level", "operational message"}, messages)
}
