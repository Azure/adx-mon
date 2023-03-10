package engine

import (
	"context"
	"github.com/Azure/adx-mon/alert"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	"github.com/Azure/azure-kusto-go/kusto/data/types"
	"github.com/Azure/azure-kusto-go/kusto/data/value"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExecutor_Handler_MissingTitle(t *testing.T) {
	e := Executor{
		AlertCli: &fakeAlertClient{},
	}

	rule := rules.Rule{}

	iter := &kusto.RowIterator{}

	rows, err := kusto.NewMockRows(table.Columns{
		{Name: "Severity", Type: types.Long},
		{Name: "Summary", Type: types.String},
		{Name: "CorrelationId", Type: types.String},
	})
	require.NoError(t, err)

	rows.Row(value.Values{
		value.Long{Value: 1, Valid: true},
		value.String{Value: "Fake Alert Summary", Valid: true},
		value.String{Value: "Fake CorrelationId", Valid: true},
	})

	require.NoError(t, iter.Mock(rows))

	row, _, _ := iter.NextRowOrError()
	require.ErrorContains(t, e.HandlerFn("http://endpoint", rule, row), "title must be between 1 and 512 chars")
}

func TestExecutor_Handler_Severity(t *testing.T) {

	for _, tt := range []struct {
		desc     string
		columns  []table.Column
		rows     value.Values
		err      string
		severity int
	}{
		{
			desc:    "missing severity",
			columns: table.Columns{{Name: "Title", Type: types.String}},
			rows:    value.Values{value.String{Value: "Title", Valid: true}},
			err:     "severity must be specified",
		},
		{
			desc: "severity as long",
			columns: table.Columns{
				{Name: "Title", Type: types.String},
				{Name: "Severity", Type: types.Long}},
			rows: value.Values{value.String{Value: "Title", Valid: true},
				value.Long{Value: 1, Valid: false}},
			err:      "",
			severity: 1,
		},
		{
			desc: "severity as string",
			columns: table.Columns{
				{Name: "Title", Type: types.String},
				{Name: "Severity", Type: types.String}},
			rows: value.Values{value.String{Value: "Title", Valid: true},
				value.String{Value: "10", Valid: false}},
			err:      "",
			severity: 10,
		},
		{
			desc: "severity as real",
			columns: table.Columns{
				{Name: "Title", Type: types.String},
				{Name: "Severity", Type: types.Real}},
			rows: value.Values{value.String{Value: "Title", Valid: true},
				value.Real{Value: 10.1, Valid: false}},
			err:      "",
			severity: 10,
		},
		{
			desc: "severity as int",
			columns: table.Columns{
				{Name: "Title", Type: types.String},
				{Name: "Severity", Type: types.Int}},
			rows: value.Values{value.String{Value: "Title", Valid: true},
				value.Int{Value: 10, Valid: false}},
			err:      "",
			severity: 10,
		},
		{
			desc: "severity as decimal",
			columns: table.Columns{
				{Name: "Title", Type: types.String},
				{Name: "Severity", Type: types.Decimal}},
			rows: value.Values{value.String{Value: "Title", Valid: true},
				value.Decimal{Value: "10.1", Valid: false}},
			err:      "",
			severity: 10,
		},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			iter := &kusto.RowIterator{}

			rows, err := kusto.NewMockRows(tt.columns)
			require.NoError(t, err)

			rows.Row(tt.rows)
			require.NoError(t, iter.Mock(rows))

			row, _, _ := iter.NextRowOrError()

			client := &fakeAlertClient{}
			e := Executor{
				AlertCli: client,
			}

			rule := rules.Rule{}

			err = e.HandlerFn("http://endpoint", rule, row)
			if tt.err == "" {
				require.NoError(t, err)
				require.Equal(t, tt.severity, client.alert.Severity)
			} else {
				require.ErrorContains(t, err, tt.err)
			}
		})
	}
}

func TestExecutor_syncWorkers_Remove(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	e := Executor{
		RuleStore: &fakeRuleStore{},
		workers: map[string]*worker{
			"alert": &worker{
				ctx:    ctx,
				cancel: cancel,
			},
		},
	}

	require.Equal(t, 1, len(e.workers))
	e.syncWorkers()
	require.Equal(t, 0, len(e.workers))
}

func TestExecutor_syncWorkers_Add(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	e := Executor{
		ctx:     ctx,
		closeFn: cancel,
		RuleStore: &fakeRuleStore{
			rules: []*rules.Rule{
				&rules.Rule{
					Name: "alert",
				},
			},
		},
		workers: map[string]*worker{},
	}

	require.Equal(t, 0, len(e.workers))
	e.syncWorkers()
	require.Equal(t, 1, len(e.workers))
}

type fakeAlertClient struct {
	alert alert.Alert
}

func (f *fakeAlertClient) Create(ctx context.Context, endpoint string, alert alert.Alert) error {
	f.alert = alert
	return nil
}

type fakeRuleStore struct {
	rules []*rules.Rule
}

func (f *fakeRuleStore) Rules() []*rules.Rule {
	return f.rules
}
