package testutils

import (
	"context"
	"fmt"
	"testing"

	azkustodata "github.com/Azure/azure-kusto-go/azkustodata"
	"github.com/Azure/azure-kusto-go/azkustodata/kql"
	"github.com/Azure/azure-kusto-go/azkustodata/query"
	"github.com/stretchr/testify/require"
)

type TableSchema interface {
	TableName() string
	CslColumns() []string
}

func newKustoClient(t *testing.T, uri string) *azkustodata.Client {
	t.Helper()

	client, err := azkustodata.New(azkustodata.NewConnectionStringBuilder(uri))
	require.NoError(t, err)
	return client
}

func primaryResultRows(t *testing.T, dataset query.Dataset) []query.Row {
	t.Helper()

	for _, table := range dataset.Tables() {
		if table.IsPrimaryResult() {
			return table.Rows()
		}
	}

	require.FailNow(t, "Kusto response did not contain a primary result table")
	return nil
}

func forEachPrimaryResultRow(dataset query.IterativeDataset, visit func(query.Row) error) error {
	foundPrimary := false
	for tableResult := range dataset.Tables() {
		if err := tableResult.Err(); err != nil {
			return err
		}

		table := tableResult.Table()
		if !table.IsPrimaryResult() {
			continue
		}
		foundPrimary = true

		for rowResult := range table.Rows() {
			if err := rowResult.Err(); err != nil {
				return err
			}
			if err := visit(rowResult.Row()); err != nil {
				return err
			}
		}
	}

	if !foundPrimary {
		return fmt.Errorf("Kusto response did not contain a primary result table")
	}
	return nil
}

func TableExists(ctx context.Context, t *testing.T, database, table, uri string) bool {
	t.Helper()

	client := newKustoClient(t, uri)
	defer client.Close()

	stmt := kql.New(".show tables")
	dataset, err := client.Mgmt(ctx, database, stmt)
	require.NoError(t, err)

	for _, row := range primaryResultRows(t, dataset) {
		var tbl Table
		if err := row.ToStruct(&tbl); err != nil {
			t.Errorf("Failed to convert row to struct: %v", err)
			continue
		}
		if tbl.TableName == table {
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

	fn := GetFunction(ctx, t, database, function, uri)
	return fn.Name == function
}

func GetFunction(ctx context.Context, t *testing.T, database, function, uri string) Function {
	t.Helper()

	client := newKustoClient(t, uri)
	defer client.Close()

	stmt := kql.New(".show functions")
	dataset, err := client.Mgmt(ctx, database, stmt)
	require.NoError(t, err)

	for _, row := range primaryResultRows(t, dataset) {
		var fn Function
		if err := row.ToStruct(&fn); err != nil {
			t.Errorf("Failed to convert row to struct: %v", err)
			continue
		}
		if fn.Name == function {
			return fn
		}
	}

	return Function{}
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

	client := newKustoClient(t, uri)
	defer client.Close()

	stmt := kql.New("").AddUnsafe(table).AddLiteral(" | count")
	dataset, err := client.Query(ctx, database, stmt)
	require.NoError(t, err)

	rows := primaryResultRows(t, dataset)
	require.Len(t, rows, 1, "Expected one row count result")

	var count RowCount
	require.NoError(t, rows[0].ToStruct(&count), "Failed to convert row count to struct")

	return count.Count > 0
}

type RowCount struct {
	Count int64 `kusto:"Count"`
}

func VerifyTableSchema(ctx context.Context, t *testing.T, database, table, uri string, expect TableSchema) {
	t.Helper()

	client := newKustoClient(t, uri)
	defer client.Close()

	stmt := kql.New("").AddUnsafe(table).AddLiteral(" | getschema")
	dataset, err := client.IterativeQuery(ctx, database, stmt)
	require.NoError(t, err)
	defer dataset.Close()

	var schema []*KqlSchema
	err = forEachPrimaryResultRow(dataset, func(row query.Row) error {
		var s KqlSchema
		if err := row.ToStruct(&s); err != nil {
			return fmt.Errorf("convert schema row to struct: %w", err)
		}
		schema = append(schema, &s)
		return nil
	})
	require.NoError(t, err, "Failed to retrieve schema")

	require.Equal(t, expect.TableName(), table)
	require.Equal(t, cslSchemaFromKqlSchema(schema), expect.CslColumns())
}

type KqlSchema struct {
	ColumnName    string `kusto:"ColumnName"`
	ColumnOrdinal int    `kusto:"ColumnOrdinal"`
	DataType      string `kusto:"DataType"`
	ColumnType    string `kusto:"ColumnType"`
}

func cslSchemaFromKqlSchema(k []*KqlSchema) []string {
	var s []string
	for _, col := range k {
		s = append(s, fmt.Sprintf("%s:%s", col.ColumnName, col.ColumnType))
	}
	return s
}
