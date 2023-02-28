package engine

import (
	"context"

	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
)

type Client interface {
	Endpoint(db string) string
	Query(ctx context.Context, r rules.Rule, fn func(endpoint string, rule rules.Rule, row *table.Row) error) error
}
