package engine

import (
	"context"

	"github.com/Azure/azure-kusto-go/kusto/data/table"
)

type Client interface {
	Endpoint(db string) string
	Query(ctx context.Context, qc *QueryContext, fn func(ctx context.Context, endpoint string, qc *QueryContext, row *table.Row) error) (error, int)
	AvailableDatabases() []string
	FindCaseInsensitiveMatch(db string) string
}
