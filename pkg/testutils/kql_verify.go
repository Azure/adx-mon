package testutils

import (
	"context"
	"io"
	"testing"

	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	"github.com/stretchr/testify/require"
)

func TableExists(ctx context.Context, t *testing.T, database, table, uri string) bool {
	t.Helper()

	cb := kusto.NewConnectionStringBuilder(uri)
	client, err := kusto.New(cb)
	require.NoError(t, err)
	defer client.Close()

	stmt := kql.New(".show tables")
	rows, err := client.Mgmt(ctx, database, stmt)
	require.NoError(t, err)
	defer rows.Stop()

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
			t.Errorf("Failed to retrieve tables: %v", errFinal)
		}

		var tbl Table
		if err := row.ToStruct(&tbl); err != nil {
			t.Errorf("Failed to convert row to struct: %v", err)
			continue
		}
		if tbl.TableName == table {
			t.Logf("Found table %s in database %s", table, database)
			return true
		}
	}

	return false
}

type Table struct {
	TableName    string `kusto:"TableName"`
	DatabaseName string `kusto:"DatabaseName"`
	Folder       string `kusto:"Folder"`
	DocString    string `kusto:"DocString"`
}

func FunctionExists(ctx context.Context, t *testing.T, database, function, uri string) bool {
	t.Helper()

	cb := kusto.NewConnectionStringBuilder(uri)
	client, err := kusto.New(cb)
	require.NoError(t, err)
	defer client.Close()

	stmt := kql.New(".show functions")
	rows, err := client.Mgmt(ctx, database, stmt)
	require.NoError(t, err)
	defer rows.Stop()

	for {
		row, errInline, errFinal := rows.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			t.Logf("Partial failure to retrieve functions: %v", errInline)
			continue
		}
		if errFinal != nil {
			t.Errorf("Failed to retrieve functions: %v", errFinal)
		}

		var fn Function
		if err := row.ToStruct(&fn); err != nil {
			t.Errorf("Failed to convert row to struct: %v", err)
			continue
		}
		if fn.Name == function {
			t.Logf("Found function %s in database %s", function, database)
			return true
		}
	}

	return false
}

type Function struct {
	Name       string `kusto:"Name"`
	Parameters string `kusto:"Parameters"`
	Body       string `kusto:"Body"`
	Folder     string `kusto:"Folder"`
	DocString  string `kusto:"DocString"`
}

func TableHasRows(ctx context.Context, t *testing.T, database, table, uri string) bool {
	t.Helper()

	cb := kusto.NewConnectionStringBuilder(uri)
	client, err := kusto.New(cb)
	require.NoError(t, err)
	defer client.Close()

	query := kql.New("").AddUnsafe(table).AddLiteral(" | count")
	rows, err := client.Query(ctx, database, query)
	require.NoError(t, err)
	defer rows.Stop()

	for {
		row, errInline, errFinal := rows.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			t.Logf("Partial failure to retrieve row count: %v", errInline)
			continue
		}
		if errFinal != nil {
			t.Errorf("Failed to retrieve row count: %v", errFinal)
		}

		var count RowCount
		if err := row.ToStruct(&count); err != nil {
			t.Errorf("Failed to convert row to struct: %v", err)
			continue
		}
		t.Logf("Table %s has %d rows", table, count.Count)
		return count.Count > 0
	}

	t.Logf("Table %s has no rows", table)
	return false
}

type RowCount struct {
	Count int64 `kusto:"Count"`
}
