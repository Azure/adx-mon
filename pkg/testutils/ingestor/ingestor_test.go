package ingestor_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils/ingestor"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
)

func TestIngestor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(3*time.Minute))
	defer cancel()

	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.27.1-k3s1")
	defer testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)

	kubeConfigYaml, err := k3sContainer.GetKubeConfig(ctx)
	require.NoError(t, err)

	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, kubeConfigYaml, 0644))

	ingestor, err := ingestor.Run(
		ctx,
		"", // building from source
		ingestor.WithNamespace("default"),
		ingestor.WithKubeconfig(kubeconfigPath),
	)
	require.NoError(t, err)
	defer testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)

	state, err := ingestor.State(ctx)
	require.NoError(t, err)
	require.True(t, state.Running)

	require.NoError(t, ingestor.Terminate(ctx))
}
