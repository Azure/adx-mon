package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestMaxConnectionsDefault(t *testing.T) {
	app := newApp()
	var got uint
	app.Action = func(ctx *cli.Context) error { got = ctx.Uint("max-connections"); return nil }
	require.NoError(t, app.Run([]string{"ingestor"}))
	require.Equal(t, uint(1000), got)
}
