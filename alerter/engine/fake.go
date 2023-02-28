package engine

import (
	"context"
	"fmt"

	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
)

func NewMock(log logger.Logger) Client {
	return &mock{log: log}
}

type mock struct {
	log logger.Logger
}

func (m *mock) Endpoint(db string) string {
	return fmt.Sprintf("%s.mockcluster.kusto.windows.net", db)
}

func (m *mock) Query(ctx context.Context, r rules.Rule, fn func(string, rules.Rule, *table.Row) error) error {
	m.log.Info("Executing rule %s", r.Database)
	return nil
}
