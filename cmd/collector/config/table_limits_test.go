package config

import (
	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTableLimitConfig(t *testing.T) {
	var cfg Config
	require.NoError(t, toml.Unmarshal([]byte("max-table-disk-usage = 1024\nmax-table-segment-count = 10\n[[table-limits]]\ndatabase = 'Logs'\ntable = 'App'\nmax-bytes = 2048\nmax-segments = 20\n"), &cfg))
	require.NoError(t, cfg.Validate())
	require.Len(t, cfg.TableLimits, 1)
	require.EqualValues(t, 2048, cfg.TableLimits[0].MaxBytes)
	cfg.TableLimits[0].Table = "bad_name"
	require.Error(t, cfg.Validate())
}
