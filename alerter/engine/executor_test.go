package engine

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	"github.com/Azure/azure-kusto-go/kusto/data/types"
	"github.com/Azure/azure-kusto-go/kusto/data/value"
	"github.com/stretchr/testify/require"
)

func TestExecutor_Handler_MissingTitle(t *testing.T) {
	e := Executor{
		alertCli: &fakeAlertClient{},
	}

	rule := &rules.Rule{}
	qc := &QueryContext{
		Rule: rule,
	}

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
	require.ErrorContains(t, e.HandlerFn(context.Background(), "http://endpoint", qc, row), "title must be between 1 and 512 chars")
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
				alertCli: client,
			}

			rule := &rules.Rule{}
			qc := &QueryContext{
				Rule: rule,
			}

			err = e.HandlerFn(context.Background(), "http://endpoint", qc, row)
			if tt.err == "" {
				require.NoError(t, err)
				require.Equal(t, tt.severity, client.alert.Severity)
			} else {
				require.ErrorContains(t, err, tt.err)
			}
		})
	}
}

func TestExecutor_RunOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e := Executor{
		ruleStore: &fakeRuleStore{},
	}

	require.NotPanics(t, func() {
		e.RunOnce(ctx)
	})
	// his is only checkign we don't panic for now. Need to add rules to see we actually get alerts.

}

func TestExecutor_syncWorkers_Remove(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	e := Executor{
		ruleStore: &fakeRuleStore{},
		workers: map[string]*worker{
			"alert": &worker{
				cancel: cancel,
			},
		},
	}

	require.Equal(t, 1, len(e.workers))
	e.syncWorkers(ctx)
	require.Equal(t, 0, len(e.workers))
}

func TestExecutor_syncWorkers_Add(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	e := Executor{
		closeFn: cancel,
		ruleStore: &fakeRuleStore{
			rules: []*rules.Rule{
				{
					Name:     "alert",
					Interval: 10 * time.Second,
				},
			},
		},
		kustoClient: &fakeKustoClient{log: logger.Default},
		workers:     map[string]*worker{},
	}

	require.Equal(t, 0, len(e.workers))
	e.syncWorkers(ctx)
	require.Equal(t, 1, len(e.workers))
}

func TestExecutor_syncWorkers_NoChange(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	e := Executor{
		closeFn: cancel,
		ruleStore: &fakeRuleStore{
			rules: []*rules.Rule{
				{
					Name:     "alert",
					Interval: 10 * time.Second,
				},
			},
		},
		kustoClient: &fakeKustoClient{log: logger.Default},
		workers:     map[string]*worker{},
	}

	require.Equal(t, 0, len(e.workers))
	e.syncWorkers(ctx)
	require.Equal(t, 1, len(e.workers))
	e.syncWorkers(ctx)
	require.Equal(t, 1, len(e.workers))
}

func TestExecutor_syncWorkers_Changed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := &fakeRuleStore{
		rules: []*rules.Rule{
			{
				Name:      "alert",
				Namespace: "foo",
				Interval:  10 * time.Second,
			},
		},
	}
	e := Executor{
		closeFn:     cancel,
		ruleStore:   store,
		kustoClient: &fakeKustoClient{log: logger.Default},
		workers:     map[string]*worker{},
	}

	require.Equal(t, 0, len(e.workers))
	e.syncWorkers(ctx)
	require.Equal(t, 1, len(e.workers))
	store.rules[0] = &rules.Rule{
		Version:   "changed",
		Name:      "alert",
		Namespace: "foo",
		Interval:  20 * time.Second,
	}
	e.syncWorkers(ctx)
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
