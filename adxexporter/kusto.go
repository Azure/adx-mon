package adxexporter

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/Azure/adx-mon/pkg/kustoutil"
	azkustodata "github.com/Azure/azure-kusto-go/azkustodata"
	"github.com/Azure/azure-kusto-go/azkustodata/kql"
	azquery "github.com/Azure/azure-kusto-go/azkustodata/query"
	azvalue "github.com/Azure/azure-kusto-go/azkustodata/value"
	"k8s.io/utils/clock"
)

// KustoExecutor provides an interface for executing KQL queries.
// This matches the pattern established in SummaryRule and allows for easy testing.
type KustoExecutor interface {
	// Database returns the target database name
	Database() string
	// Endpoint returns the Kusto cluster endpoint
	Endpoint() string
	// IterativeQuery executes a KQL query and streams the results
	IterativeQuery(ctx context.Context, query azkustodata.Statement, options ...azkustodata.QueryOption) (azquery.IterativeDataset, error)
	// Mgmt executes a Kusto management command (dot-command)
	Mgmt(ctx context.Context, query azkustodata.Statement, options ...azkustodata.QueryOption) (azquery.Dataset, error)
}

// KustoClient wraps the Azure Kusto Go client to implement KustoExecutor
type KustoClient struct {
	client   *azkustodata.Client
	database string
	endpoint string
}

const DefaultQueryExecutorMaxRows = 50000

// NewKustoClient creates a new KustoClient with the given endpoint and database
func NewKustoClient(endpoint, database string) (*KustoClient, error) {
	kcsb := azkustodata.NewConnectionStringBuilder(endpoint)

	if strings.HasPrefix(endpoint, "https://") {
		kcsb.WithDefaultAzureCredential()
	}

	client, err := azkustodata.New(kcsb)
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

func (k *KustoClient) IterativeQuery(ctx context.Context, query azkustodata.Statement, options ...azkustodata.QueryOption) (azquery.IterativeDataset, error) {
	return k.client.IterativeQuery(ctx, k.database, query, options...)
}

// Mgmt executes a Kusto management command (dot-command) against the configured database
func (k *KustoClient) Mgmt(ctx context.Context, query azkustodata.Statement, options ...azkustodata.QueryOption) (azquery.Dataset, error) {
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
	dataset, err := qe.kustoClient.IterativeQuery(tCtx, stmt)
	if err != nil {
		return &QueryResult{
			Error:    fmt.Errorf("failed to execute query: %w", err),
			Duration: qe.clock.Since(start),
		}, nil
	}

	// Convert results to rows
	rows, err := qe.iterativeDatasetToRows(dataset)
	if closeErr := dataset.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("failed to close query result: %w", closeErr)
	}

	return &QueryResult{
		Rows:     rows,
		Error:    err,
		Duration: qe.clock.Since(start),
	}, nil
}

// iterativeDatasetToRows converts a streamed Kusto query dataset to a slice of row maps.
func (qe *QueryExecutor) iterativeDatasetToRows(ds azquery.IterativeDataset) ([]map[string]interface{}, error) {
	var rows []map[string]interface{}

	for tableResult := range ds.Tables() {
		if err := tableResult.Err(); err != nil {
			return rows, err
		}
		table := tableResult.Table()
		primary := table.IsPrimaryResult()

		for rowResult := range table.Rows() {
			if err := rowResult.Err(); err != nil {
				return rows, err
			}
			if !primary {
				continue
			}
			if qe.maxRows > 0 && len(rows) >= qe.maxRows {
				return rows, fmt.Errorf("query result exceeded maximum row limit (%d)", qe.maxRows)
			}

			// Convert row to map.
			row := rowResult.Row()
			columns := row.Columns()
			values := row.Values()
			rowMap := make(map[string]interface{}, len(columns))
			for i, col := range columns {
				if i < len(values) {
					rowMap[col.Name()] = materializeKustoValue(values[i])
				}
			}
			rows = append(rows, rowMap)
		}

		if primary {
			return rows, nil
		}
	}

	return rows, nil
}

// materializeKustoValue converts azkustodata value wrappers to plain Go scalars.
func materializeKustoValue(kustoValue azvalue.Kusto) interface{} {
	if isNilValue(kustoValue) {
		return nil
	}

	rawValue := kustoValue.GetValue()
	if isNilValue(rawValue) {
		return nil
	}

	value := reflect.ValueOf(rawValue)
	if value.Kind() == reflect.Ptr {
		return value.Elem().Interface()
	}

	return rawValue
}

func isNilValue(value interface{}) bool {
	if value == nil {
		return true
	}

	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
