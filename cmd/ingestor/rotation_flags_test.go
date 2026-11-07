package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

// flagDefaults returns the values the ingestor flags take when they are not set.
func flagDefaults(t *testing.T, names ...string) map[string]time.Duration {
	t.Helper()
	got := map[string]time.Duration{}
	app := newApp()
	app.Action = func(ctx *cli.Context) error {
		for _, n := range names {
			got[n] = ctx.Duration(n)
		}
		return nil
	}
	require.NoError(t, app.Run([]string{"ingestor"}))
	return got
}

func TestRotationAndLingerFlagDefaults(t *testing.T) {
	got := flagDefaults(t, "max-segment-age", "transfer-linger", "upload-linger", "max-transfer-age")
	require.Equal(t, 5*time.Minute, got["max-segment-age"])
	require.Equal(t, 5*time.Second, got["upload-linger"])
	require.Equal(t, 90*time.Second, got["max-transfer-age"])
	require.Equal(t, time.Duration(0), got["transfer-linger"], "0 falls back to --upload-linger")
}
