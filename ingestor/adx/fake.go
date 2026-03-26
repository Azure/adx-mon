package adx

import (
	"context"
	"errors"
	"testing"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/logger"
	azkustodata "github.com/Azure/azure-kusto-go/azkustodata"
	kustoerrors "github.com/Azure/azure-kusto-go/azkustodata/errors"
	azkustoquery "github.com/Azure/azure-kusto-go/azkustodata/query"
	kustov1 "github.com/Azure/azure-kusto-go/azkustodata/query/v1"
)

// fakeUploader is an Uploader that does nothing.
type fakeUploader struct {
	queue   chan *cluster.Batch
	closeFn context.CancelFunc
	db      string
}

func (f *fakeUploader) Mgmt(ctx context.Context, query azkustodata.Statement, options ...azkustodata.QueryOption) (kustov1.Dataset, error) {
	return kustov1.NewDataset(ctx, kustoerrors.OpMgmt, kustov1.V1{Tables: []kustov1.RawTable{
		{
			TableName: "Table_0",
			Columns: []kustov1.RawColumn{
				{ColumnName: "Name", DataType: "String", ColumnType: "string"},
			},
			Rows: []kustov1.RawRow{{Row: []interface{}{"ok"}}},
		},
	}})
}

func NewFakeUploader(db string) Uploader {
	return &fakeUploader{
		db:    db,
		queue: make(chan *cluster.Batch, 10000),
	}
}

func (f *fakeUploader) Open(ctx context.Context) error {
	ctx, f.closeFn = context.WithCancel(ctx)
	go f.upload(ctx)
	return nil
}

func (f *fakeUploader) Close() error {
	f.closeFn()
	return nil
}

func (f *fakeUploader) UploadQueue() chan *cluster.Batch {
	return f.queue
}

func (f *fakeUploader) Database() string {
	return f.db
}

func (f *fakeUploader) Endpoint() string {
	return "fake"
}

func (f *fakeUploader) upload(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case batch := <-f.queue:
			segments := batch.Segments

			for _, si := range segments {
				logger.Warnf("Uploading segment %s", si.Path)
			}
			if err := batch.Remove(); err != nil {
				logger.Errorf("Failed to remove batch: %s", err.Error())
			}
			batch.Release()
		}
	}
}

type fakeKustoMgmt struct {
	expectedQuery, actualQuery string
}

func (f *fakeKustoMgmt) Mgmt(ctx context.Context, db string, query azkustodata.Statement, options ...azkustodata.QueryOption) (kustov1.Dataset, error) {
	f.actualQuery = query.String()
	return kustov1.NewDataset(ctx, kustoerrors.OpMgmt, kustov1.V1{Tables: []kustov1.RawTable{
		{
			TableName: "Table_0",
			Columns: []kustov1.RawColumn{
				{ColumnName: "Name", DataType: "String", ColumnType: "string"},
			},
			Rows: []kustov1.RawRow{{Row: []interface{}{"ok"}}},
		},
	}})
}

func (f *fakeKustoMgmt) IterativeQuery(context.Context, string, azkustodata.Statement, ...azkustodata.QueryOption) (azkustoquery.IterativeDataset, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeKustoMgmt) Verify(t *testing.T) {
	if f.expectedQuery != "" && f.actualQuery != f.expectedQuery {
		t.Errorf("Expected query %s, got %s", f.expectedQuery, f.actualQuery)
	}
}
