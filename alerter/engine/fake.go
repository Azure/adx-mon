package engine

import (
	"context"
	"fmt"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
)

func NewFakeKustoClient(log logger.Logger) Client {
	return &fakeKustoClient{log: log}
}

type fakeKustoClient struct {
	log logger.Logger
}

func (m *fakeKustoClient) Endpoint(db string) string {
	return fmt.Sprintf("%s.mockcluster.kusto.windows.net", db)
}

func (m *fakeKustoClient) Query(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, *table.Row) error) error {
	m.log.Info("Executing rule %s", qc.Rule.Database)
	return nil
}
