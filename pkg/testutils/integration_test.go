package testutils_test

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	"github.com/stretchr/testify/require"
)

func TestLogSampleOTLP(t *testing.T) {
	var (
		waitFor  = 10 * time.Minute
		database = "Logs"
		table    = "Sample"
		uri      = TestCluster.KustoConnectionURL()
	)
	ctx, cancel := context.WithTimeout(context.Background(), waitFor)
	defer cancel()

	require.Eventually(t, func() bool {
		return testutils.TableExists(ctx, t, database, table, uri)
	}, waitFor, 10*time.Second)

	require.Eventually(t, func() bool {
		return testutils.TableHasRows(ctx, t, database, table, uri)
	}, waitFor, time.Second)
}

func TestLogSampleView(t *testing.T) {
	var (
		waitFor  = 10 * time.Minute
		database = "Logs"
		target   = "Sample"
		uri      = TestCluster.KustoConnectionURL()
	)
	t.Logf("Kusto URI: %s", uri)
	t.Logf("Kubeconfig: %s", TestCluster.KubeConfigPath())

	ctx, cancel := context.WithTimeout(context.Background(), waitFor)
	defer cancel()

	require.Eventually(t, func() bool {
		return testutils.TableExists(ctx, t, database, target, uri)
	}, waitFor, 10*time.Second)

	require.Eventually(t, func() bool {
		return testutils.FunctionExists(ctx, t, database, target, uri)
	}, waitFor, 30*time.Second)

	testutils.VerifyCRDFunctionInstalled(ctx, t, TestCluster.KubeConfigPath(), target)

	row := QuerySampleView(ctx, t, database, target, uri)
	row.Validate(t)
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
