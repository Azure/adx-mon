package multikustoclient

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/alerter/engine"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
)

type multiKustoClient struct {
	clients          map[string]QueryClient
	maxNotifications int
}

func New(endpoints map[string]string, configureAuth authConfiguror, max int) (multiKustoClient, error) {
	clients := make(map[string]QueryClient)
	for name, endpoint := range endpoints {
		kcsb := kusto.NewConnectionStringBuilder(endpoint)
		if strings.HasPrefix(endpoint, "https://") {
			kcsb = configureAuth(kcsb.WithAzCli())
		}
		client, err := kusto.New(kcsb)
		if err != nil {
			return multiKustoClient{}, fmt.Errorf("kusto client=%s: %w", endpoint, err)
		}
		clients[name] = client
	}
	if len(clients) == 0 {
		return multiKustoClient{}, fmt.Errorf("no kusto endpoints provided")
	}
	return multiKustoClient{clients: clients, maxNotifications: max}, nil
}

func (c multiKustoClient) Query(ctx context.Context, qc *engine.QueryContext, fn func(context.Context, string, *engine.QueryContext, *table.Row) error) (error, int) {
	client := c.clients[qc.Rule.Database]
	if client == nil {
		return &engine.UnknownDBError{DB: qc.Rule.Database}, 0
	}

	var iter *kusto.RowIterator
	var err error
	if qc.Rule.IsMgmtQuery {
		iter, err = client.Mgmt(ctx, qc.Rule.Database, qc.Stmt)
		if err != nil {
			return fmt.Errorf("failed to execute management kusto query=%s/%s: %w", qc.Rule.Namespace, qc.Rule.Name, err), 0
		}
	} else {
		iter, err = client.Query(ctx, qc.Rule.Database, qc.Stmt)
		if err != nil {
			return fmt.Errorf("failed to execute kusto query=%s/%s: %w, %s", qc.Rule.Namespace, qc.Rule.Name, err, qc.Stmt), 0
		}
	}

	var n int
	defer iter.Stop()
	if err := iter.Do(func(row *table.Row) error {
		n++
		if n > c.maxNotifications {
			return fmt.Errorf("%s/%s returned more than %d icm, throttling query. %w", qc.Rule.Namespace, qc.Rule.Name, c.maxNotifications, alert.ErrTooManyRequests)
		}

		return fn(ctx, client.Endpoint(), qc, row)
	}); err != nil {
		return err, 0
	}

	return nil, n
}

func (c multiKustoClient) Endpoint(db string) string {
	cl, ok := c.clients[db]
	if !ok {
		return "unknown"
	}
	return cl.Endpoint()
}

type QueryClient interface {
	Query(ctx context.Context, db string, query kusto.Statement, options ...kusto.QueryOption) (*kusto.RowIterator, error)
	Mgmt(ctx context.Context, db string, query kusto.Statement, options ...kusto.MgmtOption) (*kusto.RowIterator, error)
	Endpoint() string
}
