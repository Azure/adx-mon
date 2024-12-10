package testutils_test

import (
	"context"
	"sync"
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
	testutils.IntegrationTest(t)

	// An extra generous timeout for the test. The test should run in
	// about 5 minutes, but when running with the race detector, it
	// can take longer.
	wg := sync.WaitGroup{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	t.Cleanup(cancel)

	kustainerUrl := StartCluster(ctx, t)

	wg.Add(1)
	go func() {
		defer wg.Done()
		VerifyLogs(ctx, t, kustainerUrl)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		VerifyMetrics(ctx, t, kustainerUrl)
	}()

	wg.Wait()
}

func StartCluster(ctx context.Context, t *testing.T) (kustoUrl string) {
	t.Helper()
	wg := sync.WaitGroup{}

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

	wg.Add(1)
	go t.Run("Configure Kusto", func(t *testing.T) {
		defer wg.Done()

		opts := kustainer.IngestionBatchingPolicy{
			MaximumBatchingTimeSpan: 30 * time.Second,
		}
		for _, dbName := range []string{"Metrics", "Logs"} {
			require.NoError(t, kustoContainer.CreateDatabase(ctx, dbName))
			require.NoError(t, kustoContainer.SetIngestionBatchingPolicy(ctx, dbName, opts))
		}
	})

	wg.Add(1)
	go t.Run("Build and install Ingestor", func(tt *testing.T) {
		defer wg.Done()

		ingestorContainer, err := ingestor.Run(ctx, ingestor.WithCluster(ctx, k3sContainer))
		testcontainers.CleanupContainer(t, ingestorContainer)
		require.NoError(tt, err)
	})

	wg.Add(1)
	go t.Run("Build and install Collector", func(tt *testing.T) {
		defer wg.Done()

		collectorContainer, err := collector.Run(ctx, collector.WithCluster(ctx, k3sContainer))
		testcontainers.CleanupContainer(t, collectorContainer)
		require.NoError(tt, err)
	})

	kustoUrl = kustoContainer.ConnectionUrl()
	wg.Wait()
	return
}

func VerifyLogs(ctx context.Context, t *testing.T, kustainerUrl string) {
	t.Helper()
	var (
		pollInterval = time.Second
		timeout      = 5 * time.Minute
		database     = "Logs"
		table        = "Collector"
	)

	t.Run("Verify Logs", func(t *testing.T) {
		t.Run("Table exists in Kusto", func(t *testing.T) {
			require.Eventually(t, func() bool {
				return testutils.TableExists(ctx, t, database, table, kustainerUrl)
			}, timeout, pollInterval)
		})

		t.Run("Table has rows", func(t *testing.T) {
			require.Eventually(t, func() bool {
				return testutils.TableHasRows(ctx, t, database, table, kustainerUrl)
			}, timeout, pollInterval)
		})

		t.Run("View exists in Kusto", func(t *testing.T) {
			require.Eventually(t, func() bool {
				return testutils.FunctionExists(ctx, t, database, table, kustainerUrl)
			}, timeout, pollInterval)
		})

		t.Run("Verify view schema", func(t *testing.T) {
			testutils.VerifyTableSchema(ctx, t, database, table, kustainerUrl, &collector.KustoTableSchema{})
		})
	})
}

func VerifyMetrics(ctx context.Context, t *testing.T, kustainerUrl string) {
	t.Helper()
	var (
		pollInterval = time.Second
		timeout      = 5 * time.Minute
		database     = "Metrics"
		table        = "AdxmonCollectorHealthCheck"
	)

	t.Run("Verify Metrics", func(t *testing.T) {
		t.Run("Table exists in Kusto", func(t *testing.T) {
			require.Eventually(t, func() bool {
				return testutils.TableExists(ctx, t, database, table, kustainerUrl)
			}, timeout, pollInterval)
		})

		t.Run("Table has rows", func(t *testing.T) {
			require.Eventually(t, func() bool {
				return testutils.TableHasRows(ctx, t, database, table, kustainerUrl)
			}, timeout, pollInterval)
		})
	})
}
