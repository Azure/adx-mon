package config

import (
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"
)

func TestConfig_TransferBatchOptions(t *testing.T) {
	var cfg Config
	require.NoError(t, toml.Unmarshal([]byte("max-batch-segments = 100\ntransfer-batch-size = 9437184\n"), &cfg))
	require.Equal(t, 100, cfg.MaxBatchSegments)
	require.Equal(t, int64(9437184), cfg.TransferBatchSize)

	// Both are unset by default, so the collector's defaults apply.
	cfg = Config{}
	require.NoError(t, toml.Unmarshal([]byte(""), &cfg))
	require.Zero(t, cfg.MaxBatchSegments)
	require.Zero(t, cfg.TransferBatchSize)
}
