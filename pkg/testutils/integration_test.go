package testutils_test

import (
	"context"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/alerter"
	"github.com/Azure/adx-mon/pkg/testutils/collector"
	"github.com/Azure/adx-mon/pkg/testutils/ingestor"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIntegration(t *testing.T) {
	testutils.IntegrationTest(t)

	// An extra generous timeout for the test. The test should run in
	// about 5 minutes, but when running with the race detector, it
	// can take longer.
	wg := sync.WaitGroup{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	t.Cleanup(cancel)

	kustainerUrl, k3sContainer := StartCluster(ctx, t)

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

	wg.Add(1)
	go func() {
		defer wg.Done()
		VerifyAlerts(ctx, t, kustainerUrl, k3sContainer)
	}()

	wg.Wait()
}

func StartCluster(ctx context.Context, t *testing.T) (kustoUrl string, k3sContainer *k3s.K3sContainer) {
	t.Helper()

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

	t.Run("Configure Kusto", func(t *testing.T) {
		opts := kustainer.IngestionBatchingPolicy{
			MaximumBatchingTimeSpan: 30 * time.Second,
		}
		for _, dbName := range []string{"Metrics", "Logs"} {
			require.NoError(t, kustoContainer.CreateDatabase(ctx, dbName))
			require.NoError(t, kustoContainer.SetIngestionBatchingPolicy(ctx, dbName, opts))
		}
	})

	t.Run("Install Ingestor and Collector", func(tt *testing.T) {
		ingestorContainer, err := ingestor.Run(ctx, "ghcr.io/azure/adx-mon/ingestor:latest", ingestor.WithCluster(ctx, k3sContainer))
		testcontainers.CleanupContainer(t, ingestorContainer)
		require.NoError(tt, err)

		collectorContainer, err := collector.Run(ctx, "ghcr.io/azure/adx-mon/collector:latest", collector.WithCluster(ctx, k3sContainer))
		testcontainers.CleanupContainer(t, collectorContainer)
		require.NoError(tt, err)
	})

	t.Run("Build and upgrade Ingestor and Collector", func(tt *testing.T) {
		// Ensure we can build the current version of the ingestor and collector and
		// upgrade the previous version to the new.
		ingestorContainer, err := ingestor.Run(ctx, "", ingestor.WithCluster(ctx, k3sContainer))
		testcontainers.CleanupContainer(t, ingestorContainer)
		require.NoError(tt, err)

		collectorContainer, err := collector.Run(ctx, "", collector.WithCluster(ctx, k3sContainer))
		testcontainers.CleanupContainer(t, collectorContainer)
		require.NoError(tt, err)
	})

	t.Run("Build and install Alerter", func(tt *testing.T) {
		crdPath := filepath.Join(t.TempDir(), "crd.yaml")
		require.NoError(t, testutils.CopyFile("../../kustomize/bases/alertrules_crd.yaml", crdPath))
		require.NoError(t, k3sContainer.CopyFileToContainer(ctx, crdPath, filepath.Join(testutils.K3sManifests, "crd.yaml"), 0644))

		alerterContainer, err := alerter.Run(ctx, alerter.WithCluster(ctx, k3sContainer))
		testcontainers.CleanupContainer(t, alerterContainer)
		require.NoError(tt, err)
	})

	kustoUrl = kustoContainer.ConnectionUrl()
	return
}

func VerifyAlerts(ctx context.Context, t *testing.T, kustainerUrl string, k3sContainer *k3s.K3sContainer) {
	t.Helper()

	t.Run("Install rule", func(t *testing.T) {
		rule := &adxmonv1.AlertRule{
			TypeMeta: metav1.TypeMeta{
				Kind:       "AlertRule",
				APIVersion: "adx-mon.azure.com/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "testalert",
				Namespace: "adx-mon",
			},
			Spec: adxmonv1.AlertRuleSpec{
				Database:          "Logs",
				Interval:          metav1.Duration{Duration: time.Minute},
				Query:             "Collector | take 1 | extend CorrelationId=\"some-id\", Title=\"Test alert\", Severity=\"Critical\" | project Title, Severity, CorrelationId",
				AutoMitigateAfter: metav1.Duration{Duration: time.Hour},
				Destination:       "sometestdestination",
			},
		}
		_, k8sClient, err := testutils.GetKubeConfig(ctx, k3sContainer)
		require.NoError(t, err)
		require.NoError(t, k8sClient.Create(ctx, rule))
	})

	t.Run("Verify alert rule triggers", func(t *testing.T) {
		cb := kusto.NewConnectionStringBuilder(kustainerUrl)
		client, err := kusto.New(cb)
		require.NoError(t, err)
		defer client.Close()

		stmt := kql.New("AdxmonAlerterQueryHealth | where Labels['name'] == 'testalert' | where Value == 1 | count")
		require.Eventually(t, func() bool {
			rows, err := client.Query(ctx, "Metrics", stmt)
			if err != nil {
				return false
			}

			for {
				row, errInline, errFinal := rows.NextRowOrError()
				if errFinal == io.EOF {
					break
				}
				if errInline != nil {
					t.Logf("Partial failure to retrieve tables: %v", errInline)
					continue
				}
				if errFinal != nil {
					t.Logf("Failed to retrieve tables: %v", errFinal)
				}

				var res KustoCountResult
				if err := row.ToStruct(&res); err != nil {
					t.Logf("Failed to convert row to struct: %v", err)
					continue
				}
				return res.Count > 0
			}

			return false
		}, 10*time.Minute, time.Second)
	})
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

type KustoCountResult struct {
	Count int64 `kusto:"Count"`
}
