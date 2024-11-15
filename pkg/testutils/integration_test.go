package testutils_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/collector"
	"github.com/Azure/adx-mon/pkg/testutils/ingestor"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/Azure/adx-mon/pkg/testutils/sample"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
)

func TestSampleIntegration(t *testing.T) {
	// This test creates a Kubernets and Kusto cluster,
	// installs adx-mon into the Kubernetes cluster,
	// and a Pod named "sample" that emits JSON formatted
	// logs and a single metric counter. The test ensures:
	// 1. Collector scrapes logs and metrics from the sample Pod
	// 2. Ingestor ingests logs and metrics into Kusto
	// 3. Logs and metrics are available in Kusto
	// 4. Logs have the correct schema as defined in the sample CRD
	var (
		waitFor         = 10 * time.Minute
		logsDatabase    = "Logs"
		metricsDatabase = "Metrics"
		logsTable       = "Sample"
		metricsTable    = "SampleIterationTotal"
	)

	ctx, cancel := context.WithTimeout(context.Background(), waitFor)
	defer cancel()

	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)

	kustoContainer, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithCluster(ctx, k3sContainer))
	testcontainers.CleanupContainer(t, kustoContainer)
	require.NoError(t, err)

	restConfig, err := testutils.K8sRestConfig(ctx, k3sContainer)
	require.NoError(t, err)
	require.NoError(t, kustoContainer.PortForward(ctx, restConfig))

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

	sampleContainer, err := sample.Run(ctx, sample.WithCluster(ctx, k3sContainer))
	testcontainers.CleanupContainer(t, sampleContainer)
	require.NoError(t, err)

	dir, err := os.MkdirTemp("", ".kube")
	require.NoError(t, err)
	kubeconfig, err := testutils.WriteKubeConfig(ctx, k3sContainer, dir)
	require.NoError(t, err)

	t.Run("Logs", func(t *testing.T) {
		t.Run("Table exists in Kusto", func(t *testing.T) {
			require.Eventually(t, func() bool {
				return testutils.TableExists(ctx, t, logsDatabase, logsTable, kustoContainer.ConnectionUrl())
			}, waitFor, 10*time.Second)
		})

		t.Run("Table has rows", func(t *testing.T) {
			require.Eventually(t, func() bool {
				return testutils.TableHasRows(ctx, t, logsDatabase, logsTable, kustoContainer.ConnectionUrl())
			}, waitFor, time.Second)
		})

		t.Run("View exists in Kusto", func(t *testing.T) {
			require.Eventually(t, func() bool {
				return testutils.FunctionExists(ctx, t, logsDatabase, logsTable, kustoContainer.ConnectionUrl())
			}, waitFor, 30*time.Second)
		})

		t.Run("View has correct schema", func(t *testing.T) {
			testutils.VerifyCRDFunctionInstalled(ctx, t, kubeconfig, logsTable)
			row := QuerySampleView(ctx, t, logsDatabase, logsTable, kustoContainer.ConnectionUrl())
			row.Validate(t)
		})
	})

	t.Run("Metrics", func(t *testing.T) {
		t.Run("Table exists in Kusto", func(t *testing.T) {
			require.Eventually(t, func() bool {
				return testutils.TableExists(ctx, t, metricsDatabase, metricsTable, kustoContainer.ConnectionUrl())
			}, waitFor, 10*time.Second)
		})

		t.Run("Table has rows", func(t *testing.T) {
			require.Eventually(t, func() bool {
				return testutils.TableHasRows(ctx, t, metricsDatabase, metricsTable, kustoContainer.ConnectionUrl())
			}, waitFor, time.Second)
		})
	})

	t.Logf("Kusto URI: %s", kustoContainer.ConnectionUrl())
	t.Logf("Kubeconfig: %s", kubeconfig)
}

func QuerySampleView(ctx context.Context, t *testing.T, database, view, uri string) SampleViewRow {
	t.Helper()

	cb := kusto.NewConnectionStringBuilder(uri)
	client, err := kusto.New(cb)
	require.NoError(t, err)
	defer client.Close()

	query := kql.New("").AddUnsafe(view).AddLiteral(" | take 1")
	rows, err := client.Query(ctx, database, query)
	require.NoError(t, err)
	defer rows.Stop()

	for {
		row, errInline, errFinal := rows.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			t.Logf("Partial failure to retrieve view: %v", errInline)
			continue
		}
		if errFinal != nil {
			t.Errorf("Failed to retrieve view: %v", errFinal)
		}

		var svr SampleViewRow
		if err := row.ToStruct(&svr); err != nil {
			t.Errorf("Failed to convert row to struct: %v", err)
			continue
		}
		return svr
	}

	return SampleViewRow{}
}

type SampleViewRow struct {
	Message   string    `kusto:"msg"`
	Encoding  string    `kusto:"encoding"`
	Number    int       `kusto:"number"`
	Time      time.Time `kusto:"time"`
	Level     string    `kusto:"level"`
	Host      string    `kusto:"host"`
	Namespace string    `kusto:"namespace"`
	Container string    `kusto:"container"`
}

func (s *SampleViewRow) Validate(t *testing.T) {
	t.Helper()
	require.NotEmptyf(t, s.Message, "Message is empty: %v", s)
	require.NotEmptyf(t, s.Encoding, "Encoding is empty: %v", s)
	require.NotZerof(t, s.Time, "Time is zero: %v", s)
	require.NotEmptyf(t, s.Level, "Level is empty: %v", s)
	require.NotEmptyf(t, s.Host, "Host is empty: %v", s)
	require.NotEmptyf(t, s.Namespace, "Namespace is empty: %v", s)
	require.NotEmptyf(t, s.Container, "Container is empty: %v", s)
}

func (s *SampleViewRow) String() string {
	return fmt.Sprintf("SampleViewRow{Message: %q, Encoding: %q, Number: %d, Time: %v, Level: %q, Host: %q, Namespace: %q, Container: %q}", s.Message, s.Encoding, s.Number, s.Time, s.Level, s.Host, s.Namespace, s.Container)
}
