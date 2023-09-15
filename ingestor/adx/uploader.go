package adx

import (
	"context"
	"io"
	"os"
	"sync"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/service"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/azure-kusto-go/kusto/ingest"
)

const ConcurrentUploads = 50

type Uploader interface {
	service.Component

	Database() string

	// UploadQueue returns a channel that can be used to upload files to kusto.
	UploadQueue() chan *cluster.Batch
}

type uploader struct {
	KustoCli   ingest.QueryClient
	storageDir string
	database   string
	opts       UploaderOpts
	syncer     *Syncer

	queue   chan *cluster.Batch
	closeFn context.CancelFunc

	wg        sync.WaitGroup
	mu        sync.RWMutex
	ingestors map[string]*ingest.Ingestion
	uploading map[string]struct{}
}

type UploaderOpts struct {
	StorageDir        string
	Database          string
	ConcurrentUploads int
	Dimensions        []string
	DefaultMapping    storage.SchemaMapping
	SampleType        SampleType
}

func NewUploader(kustoCli ingest.QueryClient, opts UploaderOpts) *uploader {
	syncer := NewSyncer(kustoCli, opts.Database, opts.DefaultMapping, opts.SampleType)

	return &uploader{
		KustoCli:   kustoCli,
		syncer:     syncer,
		storageDir: opts.StorageDir,
		database:   opts.Database,
		opts:       opts,
		queue:      make(chan *cluster.Batch, 10000),
		ingestors:  make(map[string]*ingest.Ingestion),
		uploading:  make(map[string]struct{}),
	}
}

func (n *uploader) Open(ctx context.Context) error {
	c, closeFn := context.WithCancel(ctx)
	n.closeFn = closeFn

	if err := n.syncer.Open(c); err != nil {
		return err
	}

	for i := 0; i < n.opts.ConcurrentUploads; i++ {
		go n.upload(c)
	}

	return nil
}

func (n *uploader) Close() error {
	n.closeFn()

	// Wait for all uploads to finish.
	n.wg.Wait()

	n.mu.Lock()
	defer n.mu.Unlock()

	for _, ing := range n.ingestors {
		ing.Close()
	}

	n.ingestors = nil

	return nil
}

func (n *uploader) UploadQueue() chan *cluster.Batch {
	return n.queue
}

func (n *uploader) Database() string {
	return n.database
}

func (n *uploader) uploadReader(reader io.Reader, database, table string) error {
	// Ensure we wait for this upload to finish.
	n.wg.Add(1)
	defer n.wg.Done()

	if err := n.syncer.EnsureTable(table); err != nil {
		return err
	}

	name, err := n.syncer.EnsureMapping(table)
	if err != nil {
		return err
	}

	n.mu.RLock()
	ingestor := n.ingestors[table]
	n.mu.RUnlock()

	if ingestor == nil {
		ingestor, err = func() (*ingest.Ingestion, error) {
			n.mu.Lock()
			defer n.mu.Unlock()

			ingestor = n.ingestors[table]
			if ingestor != nil {
				return ingestor, nil
			}

			ingestor, err = ingest.New(n.KustoCli, n.database, table)
			if err != nil {
				return nil, err
			}
			n.ingestors[table] = ingestor
			return ingestor, nil
		}()

		if err != nil {
			return err
		}
	}

	// Set up a maximum time for completion to be 10 minutes.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// uploadReader our file WITHOUT status reporting.
	// When completed, delete the file on local storage we are uploading.
	res, err := ingestor.FromReader(ctx, reader, ingest.IngestionMappingRef(name, ingest.CSV))
	if err != nil {
		return err
	}

	// time.Sleep(time.Minute)
	err = <-res.Wait(ctx)
	if err != nil {
		return err
	}
	// return os.Remove(file)
	return nil

}

func (n *uploader) upload(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case batch := <-n.queue:
			paths := batch.Paths

			if batch.Database != n.database {
				logger.Errorf("Database mismatch: %s != %s. Skipping batch", batch.Database, n.database)
				continue
			}

			func() {
				var (
					readers  = make([]io.Reader, 0, len(paths))
					files    = make([]io.Closer, 0, len(paths))
					database string
					table    string
					err      error
				)
				n.mu.Lock()
				for _, path := range paths {

					if _, ok := n.uploading[path]; ok {
						logger.Debugf("File %s is already uploading", path)
						continue
					}
					n.uploading[path] = struct{}{}

					database, table, _, err = wal.ParseFilename(path)
					if err != nil {
						logger.Errorf("Failed to parse file: %s", err.Error())
						continue
					}

					f, err := wal.NewSegmentReader(path)
					if os.IsNotExist(err) {
						// batches are not disjoint, so the same segments could be included in multiple batches.
						continue
					} else if err != nil {
						logger.Errorf("Failed to open file: %s", err.Error())
						continue
					}

					readers = append(readers, f)
					files = append(files, f)
				}
				n.mu.Unlock()

				defer func(paths []string, files []io.Closer) {
					for _, f := range files {
						f.Close()
					}

					n.mu.Lock()
					for _, path := range paths {
						delete(n.uploading, path)
					}
					n.mu.Unlock()
				}(paths, files)

				if len(readers) == 0 {
					return
				}

				mr := io.MultiReader(readers...)

				now := time.Now()
				if err := n.uploadReader(mr, database, table); err != nil {
					logger.Errorf("Failed to upload file: %s", err.Error())
					return
				}

				if logger.IsDebug() {
					logger.Debugf("Uploaded %v duration=%s", paths, time.Since(now).String())
				}

				for _, f := range paths {
					if err := os.RemoveAll(f); err != nil {
						logger.Errorf("Failed to remove file: %s", err.Error())
					}
				}
			}()

		}
	}
}
