package adx

import (
	"context"
	"fmt"
	"io"

	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/ingest"
	"github.com/Azure/azure-kusto-go/kusto/kql"
)

type directIngestReader struct {
	client   *kusto.Client
	database string
	table    string
}

func newDirectIngestReader(client *kusto.Client, database string, table string) *directIngestReader {
	return &directIngestReader{
		client:   client,
		database: database,
		table:    table,
	}
}

func (u *directIngestReader) Close() error {
	return nil
}

func (u *directIngestReader) FromFile(ctx context.Context, fPath string, options ...ingest.FileOption) (*ingest.Result, error) {
	return nil, fmt.Errorf("not implemented")
}

func (u *directIngestReader) FromReader(ctx context.Context, reader io.Reader, options ...ingest.FileOption) (*ingest.Result, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read data: %w", err)
	}

	stmt := kql.New(".ingest inline into table ").AddTable(u.table).AddLiteral(" <| ").AddUnsafe(string(data))
	if _, err = u.client.Mgmt(ctx, u.database, stmt); err != nil {
		return nil, fmt.Errorf("failed to ingest data: %w", err)
	}

	return &ingest.Result{}, nil
}
