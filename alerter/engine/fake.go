package engine

import (
	"context"
	"fmt"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
)

func NewFakeKustoClient(log logger.Logger) Client {
	return &fakeKustoClient{log: log}
}

type fakeKustoClient struct {
	log      logger.Logger
	queryErr error
}

func (m *fakeKustoClient) Endpoint(db string) string {
	return fmt.Sprintf("%s.mockcluster.kusto.windows.net", db)
}

func (m *fakeKustoClient) Query(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, *table.Row) error) (error, int) {
	if m.queryErr != nil {
		return m.queryErr, 0
	}
	m.log.Info("Executing rule %s", qc.Rule.Database)
	return nil, 1
}

type fakeAlerter struct {
	createFn func(ctx context.Context, endpoint string, alert alert.Alert) error
}

func (f *fakeAlerter) Create(ctx context.Context, endpoint string, alert alert.Alert) error {
	if f.createFn != nil {
		return f.createFn(ctx, endpoint, alert)
	}
	return nil
}
