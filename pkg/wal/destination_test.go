package wal

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDestinationIdentity(t *testing.T) {
	a, err := DestinationFromFilename("/data/Metrics_Cpu_abc_123.wal")
	require.NoError(t, err)
	b, err := DestinationFromPrefix("Metrics_Cpu_def")
	require.NoError(t, err)
	require.Equal(t, a, b)
	c, err := DestinationFromPrefix("Logs_Cpu")
	require.NoError(t, err)
	require.NotEqual(t, a, c)
	require.Equal(t, "Metrics.Cpu", a.String())
	_, err = DestinationFromFilename("invalid.wal")
	require.Error(t, err)
}
