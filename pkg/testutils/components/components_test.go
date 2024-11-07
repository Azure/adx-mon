package components_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils/components"
	"github.com/stretchr/testify/require"
)

func TestComponents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(10*time.Minute))
	defer cancel()

	components, err := components.Run(ctx, components.Options{
		K3sImage: "rancher/k3s:v1.27.1-k3s1",
	})
	require.NoError(t, err)

	kubeConfigYaml, err := components.GetKubeConfig(ctx)
	require.NoError(t, err)

	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, kubeConfigYaml, 0644))

	require.NoError(t, components.Close(ctx))
}
