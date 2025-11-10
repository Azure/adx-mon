package adxexporter

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Azure/adx-mon/pkg/kustoutil"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	"k8s.io/utils/clock"
)

// KustoExecutor provides an interface for executing KQL queries.
// This matches the pattern established in SummaryRule and allows for easy testing.
type KustoExecutor interface {
	// Database returns the target database name
	Database() string
	// Endpoint returns the Kusto cluster endpoint
	Endpoint() string
	// Query executes a KQL query and returns the results
	Query(ctx context.Context, query kusto.Statement, options ...kusto.QueryOption) (*kusto.RowIterator, error)
	// Mgmt executes a Kusto management command (dot-command)
	Mgmt(ctx context.Context, query kusto.Statement, options ...kusto.MgmtOption) (*kusto.RowIterator, error)
}

// KustoClient wraps the Azure Kusto Go client to implement KustoExecutor
type KustoClient struct {
	client   *kusto.Client
	database string
	endpoint string
}

const DefaultQueryExecutorMaxRows = 50000

// NewKustoClient creates a new KustoClient with the given endpoint and database
func NewKustoClient(endpoint, database string) (*KustoClient, error) {
	kcsb := kusto.NewConnectionStringBuilder(endpoint)

	if strings.HasPrefix(endpoint, "https://") {
		kcsb.WithDefaultAzureCredential()
	}

	client, err := kusto.New(kcsb)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kusto client: %w", err)
	}

	return &KustoClient{
		client:   client,
		database: database,
		endpoint: endpoint,
	}, nil
}

func (k *KustoClient) Database() string {
	return k.database
}

func (k *KustoClient) Endpoint() string {
	return k.endpoint
}

func (k *KustoClient) Query(ctx context.Context, query kusto.Statement, options ...kusto.QueryOption) (*kusto.RowIterator, error) {
	return k.client.Query(ctx, k.database, query, options...)
}

// Mgmt executes a Kusto management command (dot-command) against the configured database
func (k *KustoClient) Mgmt(ctx context.Context, query kusto.Statement, options ...kusto.MgmtOption) (*kusto.RowIterator, error) {
	return k.client.Mgmt(ctx, k.database, query, options...)
}

// QueryResult represents the result of a KQL query execution
type QueryResult struct {
	Rows     []map[string]interface{}
	Error    error
	Duration time.Duration
}

// QueryExecutor handles KQL query execution with time window management
type QueryExecutor struct {
	kustoClient KustoExecutor
	clock       clock.Clock
	maxRows     int
}

// NewQueryExecutor creates a new QueryExecutor
func NewQueryExecutor(kustoClient KustoExecutor) *QueryExecutor {
	return &QueryExecutor{
		kustoClient: kustoClient,
		clock:       clock.RealClock{},
		maxRows:     DefaultQueryExecutorMaxRows,
	}
}

// SetClock sets the clock for testing purposes
func (qe *QueryExecutor) SetClock(clk clock.Clock) {
	qe.clock = clk
}

// SetMaxRows overrides the maximum number of rows that will be materialized from a query result.
// A non-positive limit disables the safeguard.
func (qe *QueryExecutor) SetMaxRows(limit int) {
	qe.maxRows = limit
}

// ExecuteQuery executes a KQL query with time window parameters
func (qe *QueryExecutor) ExecuteQuery(ctx context.Context, queryBody string, startTime, endTime time.Time, clusterLabels map[string]string) (*QueryResult, error) {
	start := qe.clock.Now()

	tCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// Apply time window and cluster label substitutions to the query
	processedQuery := kustoutil.ApplySubstitutions(queryBody, startTime.Format(time.RFC3339Nano), endTime.Format(time.RFC3339Nano), clusterLabels)

	// Create KQL statement
	stmt := kql.New("").AddUnsafe(processedQuery)

	// Execute the query
	iter, err := qe.kustoClient.Query(tCtx, stmt)
	if err != nil {
		return &QueryResult{
			Error:    fmt.Errorf("failed to execute query: %w", err),
			Duration: qe.clock.Since(start),
		}, nil
	}
	defer iter.Stop()

	// Convert results to rows
	rows, err := qe.iteratorToRows(iter)

	return &QueryResult{
		Rows:     rows,
		Error:    err,
		Duration: qe.clock.Since(start),
	}, nil
}

// iteratorToRows converts a Kusto RowIterator to a slice of maps
func (qe *QueryExecutor) iteratorToRows(iter *kusto.RowIterator) ([]map[string]interface{}, error) {
	var rows []map[string]interface{}
	limit := qe.maxRows

	for {
		row, errInline, errFinal := iter.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			// Log inline error but continue processing
			continue
		}
		if errFinal != nil {
			return rows, fmt.Errorf("failed to read query results: %w", errFinal)
		}

		if limit > 0 && len(rows) >= limit {
			return rows, fmt.Errorf("query result exceeded maximum row limit (%d)", limit)
		}

		// Convert row to map
		rowMap := make(map[string]interface{})
		columns := row.ColumnNames()
		for i, colName := range columns {
			if i < len(row.Values) {
				rowMap[colName] = row.Values[i]
			}
		}
		rows = append(rows, rowMap)
	}

	return rows, nil
}
