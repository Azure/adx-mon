package testutils

import (
	"context"
	"errors"
	"testing"

	azerrors "github.com/Azure/azure-kusto-go/azkustodata/errors"
	"github.com/Azure/azure-kusto-go/azkustodata/query"
	"github.com/stretchr/testify/require"
)

func TestForEachPrimaryResultRow(t *testing.T) {
	testErr := errors.New("test error")

	tests := []struct {
		name         string
		dataset      query.IterativeDataset
		visit        func(query.Row) error
		expectedRows int
		expectedErr  error
		expectedText string
	}{
		{
			name:         "visits rows",
			dataset:      newTestIterativeDataset(primaryTestTable(query.RowResultSuccess(nil), query.RowResultSuccess(nil))),
			visit:        func(query.Row) error { return nil },
			expectedRows: 2,
		},
		{
			name:         "skips non-primary tables",
			dataset:      newTestIterativeDataset(nonPrimaryTestTable(query.RowResultSuccess(nil)), primaryTestTable(query.RowResultSuccess(nil))),
			visit:        func(query.Row) error { return nil },
			expectedRows: 1,
		},
		{
			name:         "missing primary result table",
			dataset:      newTestIterativeDataset(nonPrimaryTestTable(query.RowResultSuccess(nil))),
			visit:        func(query.Row) error { return nil },
			expectedText: "Kusto response did not contain a primary result table",
		},
		{
			name:        "table error",
			dataset:     newTestIterativeDataset(query.TableResultError(testErr)),
			visit:       func(query.Row) error { return nil },
			expectedErr: testErr,
		},
		{
			name:        "row error",
			dataset:     newTestIterativeDataset(primaryTestTable(query.RowResultError(testErr))),
			visit:       func(query.Row) error { return nil },
			expectedErr: testErr,
		},
		{
			name:         "visitor error",
			dataset:      newTestIterativeDataset(primaryTestTable(query.RowResultSuccess(nil))),
			visit:        func(query.Row) error { return testErr },
			expectedRows: 1,
			expectedErr:  testErr,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			visited := 0
			err := forEachPrimaryResultRow(test.dataset, func(row query.Row) error {
				visited++
				return test.visit(row)
			})

			require.Equal(t, test.expectedRows, visited)
			if test.expectedText != "" {
				require.EqualError(t, err, test.expectedText)
				return
			}
			if test.expectedErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, test.expectedErr)
		})
	}
}

type testIterativeDataset struct {
	query.BaseDataset
	tableResults []query.TableResult
}

func newTestIterativeDataset(results ...query.TableResult) *testIterativeDataset {
	return &testIterativeDataset{
		BaseDataset:  query.NewBaseDataset(context.Background(), azerrors.OpQuery, "QueryResult"),
		tableResults: results,
	}
}

func primaryTestTable(rows ...query.RowResult) query.TableResult {
	return newTestTableResult("QueryResult", rows)
}

func nonPrimaryTestTable(rows ...query.RowResult) query.TableResult {
	return newTestTableResult("QueryProperties", rows)
}

func newTestTableResult(kind string, rows []query.RowResult) query.TableResult {
	dataset := query.NewBaseDataset(context.Background(), azerrors.OpQuery, "QueryResult")
	table := &testIterativeTable{
		BaseTable: query.NewBaseTable(dataset, 0, "", kind, kind, nil),
		rows:      rows,
	}
	return query.TableResultSuccess(table)
}

func (d *testIterativeDataset) Tables() <-chan query.TableResult {
	results := make(chan query.TableResult, len(d.tableResults))
	for _, result := range d.tableResults {
		results <- result
	}
	close(results)
	return results
}

func (d *testIterativeDataset) ToDataset() (query.Dataset, error) {
	return nil, nil
}

func (d *testIterativeDataset) Close() error {
	return nil
}

type testIterativeTable struct {
	query.BaseTable
	rows []query.RowResult
}

func (t *testIterativeTable) Rows() <-chan query.RowResult {
	rows := make(chan query.RowResult, len(t.rows))
	for _, row := range t.rows {
		rows <- row
	}
	close(rows)
	return rows
}

func (t *testIterativeTable) ToTable() (query.Table, error) {
	return nil, nil
}
