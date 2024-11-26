package testutils

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	"github.com/stretchr/testify/require"
)

type TableSchema interface {
	TableName() string
	CslColumns() []string
}

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
		return count.Count > 0
	}

	return false
}

type RowCount struct {
	Count int64 `kusto:"Count"`
}

func VerifyTableSchema(ctx context.Context, t *testing.T, database, table, uri string, expect TableSchema) {
	t.Helper()

	cb := kusto.NewConnectionStringBuilder(uri)
	client, err := kusto.New(cb)
	require.NoError(t, err)
	defer client.Close()

	query := kql.New("").AddUnsafe(table).AddLiteral(" | getschema")
	rows, err := client.Query(ctx, database, query)
	require.NoError(t, err)
	defer rows.Stop()

	var schema []*KqlSchema
	for {
		row, errInline, errFinal := rows.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			t.Logf("Partial failure to retrieve schema: %v", errInline)
			continue
		}
		if errFinal != nil {
			t.Errorf("Failed to retrieve schema: %v", errFinal)
		}

		var s KqlSchema
		if err := row.ToStruct(&s); err != nil {
			t.Errorf("Failed to convert row to struct: %v", err)
			continue
		}
		schema = append(schema, &s)
	}

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
