package engine

import (
	"context"

	azquery "github.com/Azure/azure-kusto-go/azkustodata/query"
)

type Client interface {
	Endpoint(db string) string
	Query(ctx context.Context, qc *QueryContext, fn func(ctx context.Context, endpoint string, qc *QueryContext, row azquery.Row) error) (error, int)
	AvailableDatabases() []string
	FindCaseInsensitiveMatch(db string) string
}
