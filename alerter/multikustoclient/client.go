package multikustoclient

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/alerter/engine"
	azkustodata "github.com/Azure/azure-kusto-go/azkustodata"
	azquery "github.com/Azure/azure-kusto-go/azkustodata/query"
	azqueryv1 "github.com/Azure/azure-kusto-go/azkustodata/query/v1"
)

type multiKustoClient struct {
	clients            map[string]QueryClient
	availableDatabases []string
	maxNotifications   int
}

func New(endpoints map[string]string, configureAuth authConfiguror, max int) (multiKustoClient, error) {
	clients := make(map[string]QueryClient)
	for name, endpoint := range endpoints {
		kcsb := azkustodata.NewConnectionStringBuilder(endpoint)
		if strings.HasPrefix(endpoint, "https://") {
			kcsb = configureAuth(kcsb.WithAzCli())
		}
		client, err := azkustodata.New(kcsb)
		if err != nil {
			return multiKustoClient{}, fmt.Errorf("kusto client=%s: %w", endpoint, err)
		}
		clients[name] = client
	}
	if len(clients) == 0 {
		return multiKustoClient{}, fmt.Errorf("no kusto endpoints provided")
	}

	availableDatabases := make([]string, 0, len(clients))
	for name := range clients {
		availableDatabases = append(availableDatabases, name)
	}
	sort.Strings(availableDatabases)

	return multiKustoClient{
		clients:            clients,
		availableDatabases: availableDatabases,
		maxNotifications:   max,
	}, nil
}

func (c multiKustoClient) Query(ctx context.Context, qc *engine.QueryContext, fn func(context.Context, string, *engine.QueryContext, azquery.Row) error) (error, int) {
	client := c.clients[qc.Rule.Database]
	if client == nil {
		return &engine.UnknownDBError{
			DB:                   qc.Rule.Database,
			AvailableDatabases:   c.availableDatabases,
			CaseInsensitiveMatch: c.FindCaseInsensitiveMatch(qc.Rule.Database),
		}, 0
	}

	if qc.Rule.IsMgmtQuery {
		ds, err := client.Mgmt(ctx, qc.Rule.Database, qc.Stmt, queryOptions(qc)...)
		if err != nil {
			return fmt.Errorf("failed to execute management kusto query=%s/%s: %w", qc.Rule.Namespace, qc.Rule.Name, err), 0
		}
		return c.handleDatasetRows(ctx, client.Endpoint(), qc, ds, fn)
	}

	ds, err := client.IterativeQuery(ctx, qc.Rule.Database, qc.Stmt, queryOptions(qc)...)
	if err != nil {
		return fmt.Errorf("failed to execute kusto query=%s/%s: %w, %s", qc.Rule.Namespace, qc.Rule.Name, err, qc.Stmt), 0
	}
	defer ds.Close()

	return c.handleIterativeRows(ctx, client.Endpoint(), qc, ds, fn)
}

func queryOptions(qc *engine.QueryContext) []azkustodata.QueryOption {
	if qc.Params == nil {
		return nil
	}
	return []azkustodata.QueryOption{azkustodata.QueryParameters(qc.Params)}
}

// handleIterativeRows processes the rows from an iterative dataset and calls the provided function for each row.
// If we get an error, we return prior to emitting any rows to avoid invalid alerts being emitted.
// In some cases, Kusto will return some rows along with InternalServerError which are cases when incomplete rows have been processed, so some rows may be returned to us incorrectly.
func (c multiKustoClient) handleIterativeRows(ctx context.Context, endpoint string, qc *engine.QueryContext, ds azquery.IterativeDataset, fn func(context.Context, string, *engine.QueryContext, azquery.Row) error) (error, int) {
	var rows []azquery.Row
	tooManyRows := false

	for tableResult := range ds.Tables() {
		if tableResult.Err() != nil {
			return tableResult.Err(), 0
		}

		table := tableResult.Table()
		if !table.IsPrimaryResult() {
			continue
		}

		for rowResult := range table.Rows() {
			if rowResult.Err() != nil {
				return rowResult.Err(), 0
			}

			if len(rows) >= c.maxNotifications {
				tooManyRows = true
				continue // consume the rest of the rows to consume the rest of the body
			}
			rows = append(rows, rowResult.Row())
		}
	}

	return c.emitRows(ctx, endpoint, qc, rows, tooManyRows, fn)
}

func (c multiKustoClient) handleDatasetRows(ctx context.Context, endpoint string, qc *engine.QueryContext, ds azquery.Dataset, fn func(context.Context, string, *engine.QueryContext, azquery.Row) error) (error, int) {
	var rows []azquery.Row
	tooManyRows := false

	for _, table := range ds.Tables() {
		if !table.IsPrimaryResult() {
			continue
		}

		for _, row := range table.Rows() {
			if len(rows) >= c.maxNotifications {
				tooManyRows = true
				continue
			}
			rows = append(rows, row)
		}
	}

	return c.emitRows(ctx, endpoint, qc, rows, tooManyRows, fn)
}

func (c multiKustoClient) emitRows(ctx context.Context, endpoint string, qc *engine.QueryContext, rows []azquery.Row, tooManyRows bool, fn func(context.Context, string, *engine.QueryContext, azquery.Row) error) (error, int) {
	for _, row := range rows {
		if err := fn(ctx, endpoint, qc, row); err != nil {
			return err, 0
		}
	}

	if tooManyRows {
		return fmt.Errorf("%s/%s returned more than %d icm, throttling query. %w", qc.Rule.Namespace, qc.Rule.Name, c.maxNotifications, alert.ErrTooManyRequests), 0
	}

	return nil, len(rows)
}

func (c multiKustoClient) Endpoint(db string) string {
	cl, ok := c.clients[db]
	if !ok {
		return "unknown"
	}
	return cl.Endpoint()
}

// AvailableDatabases returns a sorted list of all configured database names.
func (c multiKustoClient) AvailableDatabases() []string {
	return c.availableDatabases
}

// FindCaseInsensitiveMatch returns a database name that matches the given db name
// case-insensitively, or an empty string if no match is found.
func (c multiKustoClient) FindCaseInsensitiveMatch(db string) string {
	lowerDB := strings.ToLower(db)
	for name := range c.clients {
		if strings.ToLower(name) == lowerDB {
			return name
		}
	}
	return ""
}

type QueryClient interface {
	IterativeQuery(ctx context.Context, db string, query azkustodata.Statement, options ...azkustodata.QueryOption) (azquery.IterativeDataset, error)
	Mgmt(ctx context.Context, db string, query azkustodata.Statement, options ...azkustodata.QueryOption) (azqueryv1.Dataset, error)
	Endpoint() string
}
