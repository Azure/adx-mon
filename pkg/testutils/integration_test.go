package testutils_test

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/collector"
	"github.com/Azure/adx-mon/pkg/testutils/ingestor"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
)

func TestIntegration(t *testing.T) {
	// An extra generous timeout for the test. The test should run in
	// about 5 minutes, but when running with the race detector, it
	// can take longer.
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)

	kustoContainer, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithCluster(ctx, k3sContainer))
	testcontainers.CleanupContainer(t, kustoContainer)
	require.NoError(t, err)

	restConfig, _, err := testutils.GetKubeConfig(ctx, k3sContainer)
	require.NoError(t, err)
	require.NoError(t, kustoContainer.PortForward(ctx, restConfig))

	kubeconfig, err := testutils.WriteKubeConfig(ctx, k3sContainer, t.TempDir())
	require.NoError(t, err)
	t.Logf("Kubeconfig: %s", kubeconfig)
	t.Logf("Kustainer: %s", kustoContainer.ConnectionUrl())

	t.Run("Create databases", func(t *testing.T) {
		for _, dbName := range []string{"Metrics", "Logs"} {
			require.NoError(t, kustoContainer.CreateDatabase(ctx, dbName))
		}
	})

	ingestorContainer, err := ingestor.Run(ctx, ingestor.WithCluster(ctx, k3sContainer))
	testcontainers.CleanupContainer(t, ingestorContainer)
	require.NoError(t, err)

	collectorContainer, err := collector.Run(ctx, collector.WithCluster(ctx, k3sContainer))
	testcontainers.CleanupContainer(t, collectorContainer)
	require.NoError(t, err)

	t.Run("Logs", func(t *testing.T) {
		var (
			pollInterval = time.Second
			timeout      = 5 * time.Minute
			database     = "Logs"
			table        = "Collector"
		)

		t.Run("Table exists in Kusto", func(t *testing.T) {
			require.Eventually(t, func() bool {
				return testutils.TableExists(ctx, t, database, table, kustoContainer.ConnectionUrl())
			}, timeout, pollInterval)
		})

		t.Run("Table has rows", func(t *testing.T) {
			require.Eventually(t, func() bool {
				return testutils.TableHasRows(ctx, t, database, table, kustoContainer.ConnectionUrl())
			}, timeout, pollInterval)
		})

		t.Run("View exists in Kusto", func(t *testing.T) {
			require.Eventually(t, func() bool {
				return testutils.FunctionExists(ctx, t, database, table, kustoContainer.ConnectionUrl())
			}, timeout, pollInterval)
		})

		t.Run("Verify view schema", func(t *testing.T) {
			testutils.VerifyTableSchema(ctx, t, database, table, kustoContainer.ConnectionUrl(), &collector.KustoTableSchema{})
		})
	})

	t.Run("Metrics", func(t *testing.T) {
		var (
			pollInterval = time.Second
			timeout      = 5 * time.Minute
			database     = "Metrics"
			table        = "AdxmonCollectorHealthCheck"
		)

		t.Run("Table exists in Kusto", func(t *testing.T) {
			require.Eventually(t, func() bool {
				return testutils.TableExists(ctx, t, database, table, kustoContainer.ConnectionUrl())
			}, timeout, pollInterval)
		})

		t.Run("Table has rows", func(t *testing.T) {
			require.Eventually(t, func() bool {
				return testutils.TableHasRows(ctx, t, database, table, kustoContainer.ConnectionUrl())
			}, timeout, pollInterval)
		})
	})
}
