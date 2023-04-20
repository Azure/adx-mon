package adx

import (
	"context"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/azure-kusto-go/kusto/ingest"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const ConcurrentUploads = 50

type Uploader interface {
	Open() error
	Close() error

	// UploadQueue returns a channel that can be used to upload files to kusto.
	UploadQueue() chan []string
}

type uploader struct {
	KustoCli   ingest.QueryClient
	storageDir string
	database   string
	syncer     *Syncer

	queue   chan []string
	closing chan struct{}

	mu        sync.RWMutex
	ingestors map[string]*ingest.Ingestion
	uploading map[string]struct{}
}

type UploaderOpts struct {
	StorageDir        string
	Database          string
	ConcurrentUploads int
	Dimensions        []string
}

func NewUploader(kustoCli ingest.QueryClient, opts UploaderOpts) *uploader {
	syncer := NewSyncer(kustoCli, opts.Database)

	return &uploader{
		KustoCli:   kustoCli,
		syncer:     syncer,
		storageDir: opts.StorageDir,
		database:   opts.Database,
		queue:      make(chan []string, opts.ConcurrentUploads),
		closing:    make(chan struct{}),
		ingestors:  make(map[string]*ingest.Ingestion),
		uploading:  make(map[string]struct{}),
	}
}

func (n *uploader) Open() error {
	if err := n.syncer.Open(); err != nil {
		return err
	}

	for i := 0; i < cap(n.queue); i++ {
		go n.upload()
	}

	return nil
}

func (n *uploader) Close() error {
	close(n.closing)

	n.mu.Lock()
	defer n.mu.Unlock()

	for _, ing := range n.ingestors {
		ing.Close()
	}

	n.ingestors = nil

	return nil
}

func (n *uploader) UploadQueue() chan []string {
	return n.queue
}

func (n *uploader) uploadReader(reader io.Reader, table string) error {
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

	//time.Sleep(time.Minute)
	err = <-res.Wait(ctx)
	if err != nil {
		return err
	}
	//return os.Remove(file)
	return nil

}

func (n *uploader) upload() error {
	for {
		select {
		case <-n.closing:
			return nil
		case paths := <-n.queue:

			func() {
				readers := make([]io.Reader, 0, len(paths))
				files := make([]*os.File, 0, len(paths))
				var fields []string
				n.mu.Lock()
				for _, path := range paths {

					if _, ok := n.uploading[path]; ok {
						continue
					}
					n.uploading[path] = struct{}{}

					fileName := filepath.Base(path)
					fields = strings.Split(fileName, "_")

					f, err := os.Open(path)
					if err != nil {
						logger.Error("Failed to open file: %s", err.Error())
						continue
					}
					readers = append(readers, f)
					files = append(files, f)
				}
				n.mu.Unlock()

				defer func(paths []string, files []*os.File) {
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
				if err := n.uploadReader(mr, fields[0]); err != nil {
					logger.Error("Failed to upload file: %s", err.Error())
					return
				}
				logger.Info("Uploaded %v duration=%s", paths, time.Since(now).String())
				for _, f := range paths {
					if err := os.RemoveAll(f); err != nil {
						logger.Error("Failed to remove file: %s", err.Error())
					}
				}
			}()

		}
	}
}
