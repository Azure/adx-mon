package e2e_logs

import (
	"testing"

	"github.com/Azure/adx-mon/tools/test/testutils"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	testutils.MainKustoIntegrationTest(m)
}

func TestNested(t *testing.T) {
	testutils.KustoIntegrationTestEnabled(t)

	q := kql.New("TestNested").
		AddLiteral(" | extend _log = todynamic(Body['log'])").
		AddLiteral(" | extend _message = tostring(_log['message'])").
		AddLiteral(" | extend _nested = todynamic(_log['nested'])").
		AddLiteral(" | extend _objectone = todynamic(_nested['objectone'])").
		AddLiteral(" | extend _key = tostring(_objectone['key'])").
		AddLiteral(" | project _key")

	type res struct {
		Key string `kusto:"_key"`
	}
	var results []res
	testutils.QueryKusto(t, q, func(row *table.Row) error {
		var r res
		if err := row.ToStruct(&r); err != nil {
			return err
		}
		results = append(results, r)
		return nil
	})

	require.NotEmpty(t, results)
	require.Equal(t, "value", results[0].Key)
}

func TestTypes(t *testing.T) {
	testutils.KustoIntegrationTestEnabled(t)

	q := kql.New("TestTypes").
		AddLiteral(" | extend Log = todynamic(Body['Log'])").
		AddLiteral(" | extend Str = tostring(Log['Str'])").
		AddLiteral(" | extend Int = toint(Log['Int'])").
		AddLiteral(" | extend Bool = tobool(Log['Bool'])").
		AddLiteral(" | extend Nested = todynamic(Log['Nested'])").
		AddLiteral(" | extend A = tostring(Nested['A'])").
		AddLiteral(" | project Log, Str, Int, Bool, Nested, A").
		AddLiteral(" | limit 1")

	type res struct {
		Log struct {
			Str    string `kusto:"Str"`
			Int    string `kusto:"Int"`
			Bool   string `kusto:"Bool"`
			Nested struct {
				A string `kusto:"A"`
			} `kusto:"Nested"`
		} `kusto:"Log"`
		Str    string `kusto:"Str"`
		Int    int    `kusto:"Int"`
		Bool   bool   `kusto:"Bool"`
		Nested struct {
			A string `kusto:"A"`
		} `kusto:"Nested"`
		A string `kusto:"A"`
	}
	var results []res
	testutils.QueryKusto(t, q, func(row *table.Row) error {
		var r res
		if err := row.ToStruct(&r); err != nil {
			return err
		}
		results = append(results, r)
		return nil
	})

	require.NotEmpty(t, results)
}
