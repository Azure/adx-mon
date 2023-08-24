package adx

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
)

// fakeUploader is an Uploader that does nothing.
type fakeUploader struct {
	database string
	queue    chan []string
	closeFn  context.CancelFunc
	observer map[string]map[string]int
}

func NewFakeUploader(database string) Uploader {
	return &fakeUploader{
		queue:    make(chan []string),
		observer: make(map[string]map[string]int),
		database: database,
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

func (f *fakeUploader) UploadQueue() chan []string {
	return f.queue
}

func (f *fakeUploader) Database() string {
	return f.database
}

func (f *fakeUploader) Upload(ctx context.Context, database, table string, reader io.Reader) error {
	if _, ok := f.observer[database]; !ok {
		f.observer[database] = make(map[string]int)
	}
	f.observer[database][table]++
	return nil
}

func (f *fakeUploader) upload(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case files := <-f.queue:
			for _, file := range files {
				logger.Warn("Uploading file %s", file)
				if err := os.RemoveAll(file); err != nil {
					logger.Error("Failed to remove file: %s", err.Error())
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
