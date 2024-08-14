package adx

import (
	"context"
	"flag"
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
	db      string
}

func (f *fakeUploader) Mgmt(ctx context.Context, query kusto.Statement, options ...kusto.MgmtOption) (*kusto.RowIterator, error) {
	if !isTest() {
		flag.String("test.v", "true", "fake")
	}

	rows, err := kusto.NewMockRows(table.Columns{
		{Name: "Name", Type: "string"},
	})
	if err != nil {
		return nil, err
	}
	iter := &kusto.RowIterator{}
	iter.Mock(rows)
	return iter, nil
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

func (f *fakeUploader) upload(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case batch := <-f.queue:
			segments := batch.Segments

			for _, si := range segments {
				logger.Warnf("Uploading segment %s", si.Path)
				if err := os.RemoveAll(si.Path); err != nil {
					logger.Errorf("Failed to remove segment: %s", err.Error())
				}
			}
			batch.Release()
		}
	}
}

type fakeKustoMgmt struct {
	expectedQuery, actualQuery string
}

func (f *fakeKustoMgmt) Mgmt(ctx context.Context, db string, query kusto.Statement, options ...kusto.MgmtOption) (*kusto.RowIterator, error) {
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
func isTest() bool {
	return flag.Lookup("test.v") != nil
}
