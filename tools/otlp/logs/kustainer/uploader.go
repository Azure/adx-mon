package kustainer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Azure/adx-mon/ingestor/adx"
	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/unsafe"
)

// Uploader implements an adx.Uploader for use with Kustainer,
// which is a volatile storage Kusto that is only meant for
// testing purposes.
type Uploader struct {
	DatabaseName string
	endpoint     string
	q            chan *cluster.Batch
	client       *kusto.Client
	syncer       *adx.Syncer
}

func New(databaseName, endpoint string) *Uploader {
	return &Uploader{
		DatabaseName: databaseName,
		endpoint:     endpoint,
		q:            make(chan *cluster.Batch, 1000),
	}
}

func (u *Uploader) Open(ctx context.Context) error {
	builder := kusto.NewConnectionStringBuilder(u.endpoint)
	client, err := kusto.New(builder)
	if err != nil {
		return fmt.Errorf("failed to create kustainer client: %w", err)
	}
	u.client = client

	// Wait for Kustainer to come online
	for {
		stmt := fmt.Sprintf(".create database %s volatile", u.DatabaseName)
		cmd := kusto.NewStmt("", kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).UnsafeAdd(stmt)
		_, err = u.client.Mgmt(ctx, u.DatabaseName, cmd)
		if err != nil {
			logger.Warnf("Waiting for Kustainer to come online")
			time.Sleep(5 * time.Second)
		}
		break
	}

	u.syncer = adx.NewSyncer(client, u.DatabaseName, storage.DefaultLogsMapping, adx.OTLPLogs)
	if err := u.syncer.Open(ctx); err != nil {
		return fmt.Errorf("failed to open syncer: %w", err)
	}
	go u.run(ctx)

	return nil
}

func (u *Uploader) Close() error {
	close(u.q)
	return u.client.Close()
}

func (u *Uploader) Database() string {
	return u.DatabaseName
}

func (u *Uploader) UploadQueue() chan *cluster.Batch {
	return u.q
}

func (u *Uploader) Mgmt(context.Context, kusto.Statement, ...kusto.QueryOption) (*kusto.RowIterator, error) {
	return nil, errors.New("kustainer mgmt not implemented yet")
}

func (u *Uploader) run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case batch := <-u.q:

			if batch.Database != u.DatabaseName {
				logger.Errorf("Batch database does not match uploader database: %s != %s", batch.Database, u.DatabaseName)
				continue
			}
			segments := batch.Segments

			for _, si := range segments {
				if err := u.uploadSegment(ctx, si.Path); err != nil {
					logger.Errorf("Failed to upload segment: %s", err.Error())
					continue
				}
			}
			batch.Release()
		}
	}
}

func (u *Uploader) uploadSegment(ctx context.Context, path string) error {
	_, table, _, err := wal.ParseFilename(path)
	if err != nil {
		return fmt.Errorf("failed to parse file: %w", err)
	}

	f, err := wal.NewSegmentReader(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	d, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	if err := u.syncer.EnsureTable(table); err != nil {
		return fmt.Errorf("failed to ensure table %s: %w", table, err)
	}
	if _, err := u.syncer.EnsureMapping(table); err != nil {
		return fmt.Errorf("failed to ensure mapping for table %s: %w", table, err)
	}

	stmt := fmt.Sprintf(".ingest inline into table %s <| %s", table, d)
	cmd := kusto.NewStmt("", kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).UnsafeAdd(stmt)
	_, err = u.client.Mgmt(ctx, u.DatabaseName, cmd)
	if err != nil {
		return fmt.Errorf("failed to upload segment: %w", err)
	}

	return nil
}
