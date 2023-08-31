package adx

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
)

// fakeUploader is an Uploader that does nothing.
type fakeUploader struct {
	queue   chan *cluster.Batch
	closeFn context.CancelFunc
}

func NewFakeUploader() Uploader {
	return &fakeUploader{
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
	return "FakeDatabase"
}

func (f *fakeUploader) upload(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case batch := <-f.queue:
			files := batch.Paths

			for _, file := range files {
				logger.Warnf("Uploading file %s", file)
				if err := os.RemoveAll(file); err != nil {
					logger.Errorf("Failed to remove file: %s", err.Error())
				}
			}
		}
	}
}

type fakeKustoMgmt struct {
	expectedQuery, actualQuery string
}

func (f *fakeKustoMgmt) Mgmt(ctx context.Context, db string, query kusto.Stmt, options ...kusto.MgmtOption) (*kusto.RowIterator, error) {
	f.actualQuery = query.String()

	rows, err := kusto.NewMockRows(table.Columns{
		{Name: "Name", Type: "string"},
	})
	if err != nil {
		return nil, err
	}

	iter := &kusto.RowIterator{}
	rows.Error(io.EOF)
	iter.Mock(rows)
	return iter, nil
}

func (f *fakeKustoMgmt) Verify(t *testing.T) {
	if f.expectedQuery != "" && f.actualQuery != f.expectedQuery {
		t.Errorf("Expected query %s, got %s", f.expectedQuery, f.actualQuery)
	}
}
