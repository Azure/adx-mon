package main

import (
	"github.com/Azure/adx-mon/ingestor"
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

// The flags must reach the service options realMain passes to NewService;
// validating them alone leaves enforcement and the intake ceiling disabled.
func TestTableIsolationFlagsReachServiceOpts(t *testing.T) {
	run := func(args ...string) ingestor.ServiceOpts {
		t.Helper()
		var opts ingestor.ServiceOpts
		app := newApp()
		app.Action = func(ctx *cli.Context) error {
			isolation, err := parseTableIsolationOpts(ctx)
			if err != nil {
				return err
			}
			isolation.apply(&opts)
			return nil
		}
		require.NoError(t, app.Run(append([]string{"ingestor"}, args...)))
		return opts
	}

	opts := run(
		"--max-table-disk-usage", "1000",
		"--max-table-segment-count", "10",
		"--disable-table-limits",
		"--enforce-table-limits",
		"--max-intake-memory", "2048",
		"--table-limit", `{"database":"Logs","table":"A","max-bytes":4096}`,
	)
	require.EqualValues(t, 1000, opts.MaxTableDiskUsage)
	require.EqualValues(t, 10, opts.MaxTableSegmentCount)
	require.True(t, opts.DisableTableLimits)
	require.True(t, opts.EnforceTableLimits)
	require.EqualValues(t, 2048, opts.MaxIntakeMemory)
	require.Equal(t, []wal.TableLimit{{Database: "Logs", Table: "A", MaxBytes: 4096}}, opts.TableLimits)

	defaults := run()
	require.EqualValues(t, 1<<30, defaults.MaxIntakeMemory, "the documented intake default reaches the service")
	require.False(t, defaults.EnforceTableLimits, "table budgets are observe-only unless enabled")
}

func TestTableIsolationFlagsAreValidated(t *testing.T) {
	app := newApp()
	app.Action = func(ctx *cli.Context) error {
		_, err := parseTableIsolationOpts(ctx)
		return err
	}
	require.ErrorContains(t, app.Run([]string{"ingestor", "--max-table-disk-usage", "-1"}), "must not be negative")
}
