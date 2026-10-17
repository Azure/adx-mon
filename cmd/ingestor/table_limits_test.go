package main

import (
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	"testing"
)

func TestTableLimitCLI(t *testing.T) {
	app := newApp()
	var limits []wal.TableLimit
	app.Action = func(ctx *cli.Context) error {
		limits = ctx.Generic("table-limit").(*tableLimitValues).limits
		return nil
	}
	require.NoError(t, app.Run([]string{"ingestor", "--table-limit", `{"database":"Logs","table":"A","max-bytes":1000,"max-segments":10}`, "--table-limit", `{"database":"Logs","table":"B","max-bytes":2000}`}))
	require.Len(t, limits, 2)
	require.EqualValues(t, 1000, limits[0].MaxBytes)
	require.Equal(t, "B", limits[1].Table)
	app = newApp()
	app.Action = func(*cli.Context) error { return nil }
	require.Error(t, app.Run([]string{"ingestor", "--table-limit", `{"unknown":1}`}))
}
