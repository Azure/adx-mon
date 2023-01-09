package adx

import (
	"context"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/ingest"
	"github.com/fsnotify/fsnotify"
	"golang.org/x/sync/errgroup"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const ConcurrentUploads = 50

type Ingestor struct {
	KustoCli   *kusto.Client
	storageDir string
	database   string
	syncer     *Syncer

	queue   chan string
	closing chan struct{}

	mu        sync.RWMutex
	ingestors map[string]*ingest.Ingestion
}

type IngesterOpts struct {
	StorageDir        string
	Database          string
	ConcurrentUploads int
	Dimensions        []string
}

func NewIngestor(kustoCli *kusto.Client, opts IngesterOpts) *Ingestor {
	syncer := NewSyncer(kustoCli, opts.Database)

	return &Ingestor{
		KustoCli:   kustoCli,
		syncer:     syncer,
		storageDir: opts.StorageDir,
		database:   opts.Database,
		queue:      make(chan string, opts.ConcurrentUploads),
		closing:    make(chan struct{}),
		ingestors:  make(map[string]*ingest.Ingestion),
	}
}

func (n *Ingestor) Open() error {
	if err := n.syncer.Open(); err != nil {
		return err
	}

	go n.watch()

	for i := 0; i < cap(n.queue); i++ {
		go n.upload()
	}

	return nil
}

func (n *Ingestor) Close() error {
	close(n.closing)

	n.mu.Lock()
	defer n.mu.Unlock()

	for _, ing := range n.ingestors {
		ing.Close()
	}

	n.ingestors = nil

	return nil
}

func (n *Ingestor) Upload(file, table string) error {
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

	// Upload our file WITHOUT status reporting.
	// When completed, delete the file on local storage we are uploading.
	res, err := ingestor.FromFile(ctx, file, ingest.IngestionMappingRef(name, ingest.CSV))
	if err != nil {
		return err
	}

	//time.Sleep(time.Minute)
	err = <-res.Wait(ctx)
	if err != nil {
		return err
	}
	return os.RemoveAll(file)

	//
	//// Upload our file WITH status reporting.
	//// When completed, delete the file on local storage we are uploading.
	//status, err := ingestor.FromFile(ctx, "/path/to/file", ingest.DeleteSource(), ingest.ReportResultToTable())
	//if err != nil {
	//	// The ingestion command failed to be sent, Do something
	//}
	//
	//err = <-status.Wait(ctx)
	//if err != nil {
	//	// the operation complete with an error
	//	if ingest.IsRetryable(err) {
	//		// Handle reties
	//	} else {
	//		// inspect the failure
	//		// statusCode, _ := ingest.GetIngestionStatus(err)
	//		// failureStatus, _ := ingest.GetIngestionFailureStatus(err)
	//	}
	//}
	//return nil
}

func (n *Ingestor) watch() {

	g, _ := errgroup.WithContext(context.Background())
	g.SetLimit(10)

	entries, err := os.ReadDir(n.storageDir)
	if err != nil {
		logger.Error("Failed to list files: %s", err.Error())
	}

	for _, v := range entries {
		if v.IsDir() || !strings.HasSuffix(v.Name(), ".gz") {
			continue
		}

		n.queue <- filepath.Join(n.storageDir, v.Name())
	}

	if err := g.Wait(); err != nil {
		logger.Error("Failed ingest recovery: %s", err.Error())
	}
	// Create new watcher.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Fatal("Failed watch: %s", err)
	}
	defer watcher.Close()

	// Start listening for events.

	watcher.Add(n.storageDir)
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			if event.Has(fsnotify.Create) {
				if strings.HasSuffix(event.Name, ".gz") {
					n.queue <- event.Name
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			logger.Error("Watch error: %s:", err)
		}

	}
}

func (n *Ingestor) upload() error {
	for {
		select {
		case <-n.closing:
			return nil
		case path := <-n.queue:
			now := time.Now()
			fileName := filepath.Base(path)
			fields := strings.Split(fileName, "_")

			if err := n.Upload(path, fields[0]); err != nil {
				logger.Error("Failed upload: %s: %s", path, err.Error())
				continue
			}
			logger.Info("Uploaded %s duration=%s", path, time.Since(now).String())

		}
	}
}
