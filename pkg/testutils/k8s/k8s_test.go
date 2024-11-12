package k8s_test

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/collector"
	"github.com/Azure/adx-mon/pkg/testutils/ingestor"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/Azure/adx-mon/pkg/testutils/sample"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
)

func TestK8sWithStack(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(10*time.Minute))
	defer cancel()

	// Kubernetes
	k, err := k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	testcontainers.CleanupContainer(t, k)
	require.NoError(t, err)

	kubeConfigPath, err := testutils.WriteKubeConfig(ctx, k, t.TempDir())
	require.NoError(t, err)
	t.Logf("Kubeconfig: %s", kubeConfigPath)

	// Logging container
	s, err := sample.Run(ctx, sample.WithCluster(ctx, k))
	testcontainers.CleanupContainer(t, s)
	require.NoError(t, err)

	// Kustainer
	kc, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithCluster(ctx, k))
	testcontainers.CleanupContainer(t, kc)
	require.NoError(t, err)

	restConfig, err := testutils.K8sRestConfig(ctx, k)
	require.NoError(t, err)
	require.NoError(t, kc.PortForward(ctx, restConfig))
	t.Logf("Kusto endpoint: %s", kc.ConnectionUrl())

	for _, dbName := range []string{"Metrics", "Logs"} {
		require.NoError(t, kc.CreateDatabase(ctx, dbName))
	}

	// Ingestor
	i, err := ingestor.Run(ctx, ingestor.WithCluster(ctx, k))
	testcontainers.CleanupContainer(t, i)
	require.NoError(t, err)

	// Collector
	c, err := collector.Run(ctx, collector.WithCluster(ctx, k))
	testcontainers.CleanupContainer(t, c)
	require.NoError(t, err)

	t.Log("=========== debug ===========")
	time.Sleep(time.Hour)
}
