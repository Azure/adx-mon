package logger_test

import (
	"log/slog"
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
