package multikustoclient

import (
	"context"
	"fmt"

	"github.com/Azure/adx-mon/alerter/engine"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
)

type multiKustoClient struct {
	clients          map[string]*kusto.Client
	maxNotifications int
}

func New(endpoints map[string]string, missid string, max int) (multiKustoClient, error) {
	var clients map[string]*kusto.Client
	for name, endpoint := range endpoints {
		kcsb := kusto.NewConnectionStringBuilder(endpoint).WithAzCli()
		if missid == "" {
			kcsb.WithAzCli()
		} else {
			kcsb.WithUserManagedIdentity(missid)
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

func (c multiKustoClient) Query(ctx context.Context, qc *engine.QueryContext, fn func(context.Context, string, *engine.QueryContext, *table.Row) error) error {
	client := c.clients[qc.Rule.Database]
	if client == nil {
		return fmt.Errorf("no client found for database=%s", qc.Rule.Database)
	}

	var iter *kusto.RowIterator
	var err error
	if qc.Rule.IsMgmtQuery {
		iter, err = client.Mgmt(ctx, qc.Rule.Database, qc.Stmt)
		if err != nil {
			return fmt.Errorf("failed to execute management kusto query=%s/%s: %w", qc.Rule.Namespace, qc.Rule.Name, err)
		}
	} else {
		iter, err = client.Query(ctx, qc.Rule.Database, qc.Stmt, kusto.ResultsProgressiveDisable())
		if err != nil {
			return fmt.Errorf("failed to execute kusto query=%s/%s: %w, %s", qc.Rule.Namespace, qc.Rule.Name, err, qc.Stmt)
		}
	}

	var n int
	defer iter.Stop()
	if err := iter.Do(func(row *table.Row) error {
		n++
		if n > c.maxNotifications {
			metrics.NotificationUnhealthy.WithLabelValues(qc.Rule.Namespace, qc.Rule.Name).Set(1)
			return fmt.Errorf("%s/%s returned more than %d icm, throttling query", qc.Rule.Namespace, qc.Rule.Name, c.maxNotifications)
		}

		return fn(ctx, client.Endpoint(), qc, row)
	}); err != nil {
		return err
	}

	// reset health metric since we didn't get any errors
	metrics.NotificationUnhealthy.WithLabelValues(qc.Rule.Namespace, qc.Rule.Name).Set(0)
	return nil
}

func (c multiKustoClient) Endpoint(db string) string {
	cl, ok := c.clients[db]
	if !ok {
		return "unknown"
	}
	return cl.Endpoint()
}
