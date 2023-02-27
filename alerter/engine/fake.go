package engine

import (
	"context"

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

func (m *mock) Endpoint() string {
	return "mockcluster.kusto.windows.net"
}

func (m *mock) Query(ctx context.Context, r rules.Rule, fn func(string, rules.Rule, *table.Row) error) error {
	m.log.Info("Executing rule %s", r.Database)
	return nil
}
