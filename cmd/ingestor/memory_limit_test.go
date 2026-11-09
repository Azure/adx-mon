package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestMemoryLimitFlagDefault(t *testing.T) {
	app := newApp()
	var got int64 = -1
	app.Action = func(ctx *cli.Context) error { got = ctx.Int64("memory-limit"); return nil }
	require.NoError(t, app.Run([]string{"ingestor"}))
	require.Zero(t, got)
}

func TestMemoryLimit(t *testing.T) {
	limited := func() (int64, bool) { return 10 << 30, true }
	unlimited := func() (int64, bool) { return 0, false }

	limit, source := memoryLimit(2<<30, "3GiB", limited)
	require.EqualValues(t, 2<<30, limit, "the flag wins")
	require.Equal(t, "--memory-limit", source)

	limit, source = memoryLimit(0, "3GiB", limited)
	require.Zero(t, limit, "GOMEMLIMIT is already applied by the runtime")
	require.Equal(t, "GOMEMLIMIT", source)

	limit, _ = memoryLimit(0, "", limited)
	require.EqualValues(t, 9<<30, limit, "90% of the container's limit")

	limit, source = memoryLimit(0, "", unlimited)
	require.Zero(t, limit, "no limit without a flag, GOMEMLIMIT or container limit")
	require.Empty(t, source)
}

func TestCgroupMemoryLimit(t *testing.T) {
	file := func(content string) string {
		p := filepath.Join(t.TempDir(), "limit")
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
		return p
	}
	n, ok := cgroupMemoryLimit([]string{file("4294967296\n")})
	require.True(t, ok)
	require.EqualValues(t, 4<<30, n)

	_, ok = cgroupMemoryLimit([]string{file("max\n")})
	require.False(t, ok, "cgroup v2 without a limit")

	_, ok = cgroupMemoryLimit([]string{file("9223372036854771712\n")})
	require.False(t, ok, "cgroup v1 without a limit")

	n, ok = cgroupMemoryLimit([]string{filepath.Join(t.TempDir(), "missing"), file("1073741824")})
	require.True(t, ok, "the first file that exists is used")
	require.EqualValues(t, 1<<30, n)

	_, ok = cgroupMemoryLimit([]string{filepath.Join(t.TempDir(), "missing")})
	require.False(t, ok)
}
