package kustainer_test

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestKustainer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(3*time.Minute))
	defer cancel()

	kustainerContainer, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest")
	defer testcontainers.CleanupContainer(t, kustainerContainer)
	require.NoError(t, err)

	state, err := kustainerContainer.State(ctx)
	require.NoError(t, err)

	require.Truef(t, state.Running, "Kustainer container is not running")
}

func TestKustainerWithDatabase(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(3*time.Minute))
	defer cancel()

	kustainerContainer, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest")
	defer testcontainers.CleanupContainer(t, kustainerContainer)
	require.NoError(t, err)

	require.NoError(t, kustainerContainer.CreateDatabase(ctx, "testdb"))
	require.NoError(t, kustainerContainer.CreateDatabase(ctx, "testdb")) // idempotent
}
