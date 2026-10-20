package main

import (
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestRealMain_RejectsNegativeRotationSweepInterval(t *testing.T) {
	flags := flag.NewFlagSet("ingestor", flag.ContinueOnError)
	flags.Duration("wal-rotation-sweep-interval", 0, "")
	require.NoError(t, flags.Set("wal-rotation-sweep-interval", (-time.Second).String()))
	ctx := cli.NewContext(cli.NewApp(), flags, nil)

	require.EqualError(t, realMain(ctx), "wal-rotation-sweep-interval must not be negative")
}
