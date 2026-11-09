package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

// The ingestor has no --memory-limit flag, so it runs with Go's default of no memory limit.
func TestMemoryLimitFlagIsNotDefined(t *testing.T) {
	app := newApp()
	app.Action = func(*cli.Context) error { return nil }
	require.Error(t, app.Run([]string{"ingestor", "--memory-limit=1"}))
}
