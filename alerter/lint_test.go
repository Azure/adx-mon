package alerter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Azure/adx-mon/alerter/engine"
	azerrors "github.com/Azure/azure-kusto-go/azkustodata/errors"
	azquery "github.com/Azure/azure-kusto-go/azkustodata/query"
	aztypes "github.com/Azure/azure-kusto-go/azkustodata/types"
	azvalue "github.com/Azure/azure-kusto-go/azkustodata/value"
	"github.com/stretchr/testify/require"
)

func TestRunLint_DuplicateReservedColumnsDifferentCase(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "alert_rule.yaml")
	rule := `---
apiVersion: adx-mon.azure.com/v1
kind: AlertRule
metadata:
  name: duplicate-columns
  namespace: namespace
spec:
  database: DB
  interval: 5m
  query: |
    Heartbeat
  autoMitigateAfter: 1h
  destination: "somewhere"
`
	require.NoError(t, os.WriteFile(filename, []byte(rule), 0o644))

	opts := &AlerterOpts{
		KustoEndpoints:   map[string]string{"DB": "http://fake.endpoint"},
		Region:           "test-region",
		Tags:             map[string]string{"env": "test"},
		MaxNotifications: 10,
	}

	err := runLint(context.Background(), opts, filename, func(opts *AlerterOpts) (engine.Client, error) {
		return fakeLintKustoClient{}, nil
	})
	require.ErrorContains(t, err, `query results include multiple columns for reserved alert field "Severity": Severity, severity`)
}

type fakeLintKustoClient struct{}

func (f fakeLintKustoClient) Endpoint(db string) string {
	return "https://fake.kusto.windows.net"
}

func (f fakeLintKustoClient) Query(ctx context.Context, qc *engine.QueryContext, fn func(context.Context, string, *engine.QueryContext, azquery.Row) error) (error, int) {
	row := lintTestRow()
	if err := fn(ctx, f.Endpoint(qc.Rule.Database), qc, row); err != nil {
		return err, 0
	}
	return nil, 1
}

func lintTestRow() azquery.Row {
	base := azquery.NewBaseDataset(context.Background(), azerrors.OpQuery, "QueryResult")
	table := azquery.NewBaseTable(base, 0, "", "QueryResult", "QueryResult", []azquery.Column{
		azquery.NewColumn(0, "Title", aztypes.String),
		azquery.NewColumn(1, "Severity", aztypes.Long),
		azquery.NewColumn(2, "severity", aztypes.Long),
	})
	return azquery.NewRow(table, 0, azvalue.Values{azvalue.NewString("Title"), azvalue.NewLong(1), azvalue.NewLong(2)})
}

func (f fakeLintKustoClient) AvailableDatabases() []string {
	return []string{"DB"}
}

func (f fakeLintKustoClient) FindCaseInsensitiveMatch(db string) string {
	return ""
}
