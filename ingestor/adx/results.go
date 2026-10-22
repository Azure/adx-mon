package adx

import (
	"fmt"

	"github.com/Azure/azure-kusto-go/azkustodata/query"
	kustov1 "github.com/Azure/azure-kusto-go/azkustodata/query/v1"
)

func primaryResultTable(ds kustov1.Dataset) (query.Table, bool) {
	tables := ds.Tables()
	if len(tables) == 0 {
		return nil, false
	}
	// azkustodata guarantees tables[0] is the primary result table.
	return tables[0], true
}

// forEachPrimaryResultRowIterative iterates over the rows of the primary result table of an iterative dataset, calling onRow for each row. If there is no primary result table, or if any error occurs, it is returned.
// Callers are responsible for closing the dataset when finished.
func forEachPrimaryResultRowIterative(ds query.IterativeDataset, onRow func(query.Row) error) error {
	tableResult, ok := <-ds.Tables()
	if !ok {
		return fmt.Errorf("missing primary result table")
	}
	if err := tableResult.Err(); err != nil {
		return err
	}

	for rowResult := range tableResult.Table().Rows() {
		if err := rowResult.Err(); err != nil {
			return err
		}
		if err := onRow(rowResult.Row()); err != nil {
			return err
		}
	}

	return nil
}
