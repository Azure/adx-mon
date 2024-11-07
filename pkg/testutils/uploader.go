package testutils

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/ingest"
	"github.com/Azure/azure-kusto-go/kusto/kql"
)

type Uploader struct {
	client   *kusto.Client
	database string
	table    string
}

// NewUploadReader implements ingest.Ingestor
func NewUploadReader(client *kusto.Client, database string, table string) *Uploader {
	return &Uploader{
		client:   client,
		database: database,
		table:    table,
	}
}

func (u *Uploader) Close() error {
	return nil
}

func (u *Uploader) FromFile(ctx context.Context, fPath string, options ...ingest.FileOption) (*ingest.Result, error) {
	return nil, errors.New("not implemented")
}

func (u *Uploader) FromReader(ctx context.Context, reader io.Reader, options ...ingest.FileOption) (*ingest.Result, error) {
	// Kustainer is not able to using a streaming ingestor as there is no storage containers backing the Kusto cluster.
	// We must instead ingest inline and thus consume the reader.
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read data: %w", err)
	}

	stmt := kql.New(".ingest inline into table ").AddTable(u.table).AddLiteral(" <| ").AddUnsafe(string(data))
	if _, err = u.client.Mgmt(ctx, u.database, stmt); err != nil {
		return nil, fmt.Errorf("failed to ingest data: %w", err)
	}

	return nil, nil
}
