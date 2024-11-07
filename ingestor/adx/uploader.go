package adx

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/service"
	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/wal"
	adxschema "github.com/Azure/adx-mon/schema"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/ingest"
)

const ConcurrentUploads = 50

type Uploader interface {
	service.Component

	Database() string

	// UploadQueue returns a channel that can be used to upload files to kusto.
	UploadQueue() chan *cluster.Batch

	// Mgmt executes a management query against the database.
	Mgmt(ctx context.Context, query kusto.Statement, options ...kusto.MgmtOption) (*kusto.RowIterator, error)
}

type uploader struct {
	KustoCli   *kusto.Client
	storageDir string
	database   string
	opts       UploaderOpts
	syncer     *Syncer

	queue   chan *cluster.Batch
	closeFn context.CancelFunc

	wg        sync.WaitGroup
	mu        sync.RWMutex
	ingestors map[string]ingest.Ingestor
}

type UploaderOpts struct {
	StorageDir        string
	Database          string
	ConcurrentUploads int
	Dimensions        []string
	DefaultMapping    adxschema.SchemaMapping
	SampleType        SampleType
	FnStore           FunctionStore

	// Kustainer is used to indicate if we should ingest into a volatile database
	// facilitated by Kustainer. The only reason you would want to set this value
	// is for testing purposes.
	Kustainer bool
}

func NewUploader(kustoCli *kusto.Client, opts UploaderOpts) *uploader {
	logger.Infof("BUGBUG Kusto Endpoint=%s, Kustainer=%t, Database=%s", kustoCli.Endpoint(), opts.Kustainer, opts.Database)

	syncer := NewSyncer(kustoCli, opts.Database, opts.DefaultMapping, opts.SampleType, opts.FnStore)

	return &uploader{
		KustoCli:   kustoCli,
		syncer:     syncer,
		storageDir: opts.StorageDir,
		database:   opts.Database,
		opts:       opts,
		queue:      make(chan *cluster.Batch, 10000),
		ingestors:  make(map[string]ingest.Ingestor),
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
	return n.syncer.Close()
}

func (n *uploader) UploadQueue() chan *cluster.Batch {
	return n.queue
}

func (n *uploader) Database() string {
	return n.database
}

func (n *uploader) Mgmt(ctx context.Context, query kusto.Statement, options ...kusto.MgmtOption) (*kusto.RowIterator, error) {
	return n.KustoCli.Mgmt(ctx, n.database, query, options...)
}

func (n *uploader) uploadReader(reader io.Reader, database, table string, mapping adxschema.SchemaMapping) error {
	// Ensure we wait for this upload to finish.
	n.wg.Add(1)
	defer n.wg.Done()

	if err := n.syncer.EnsureTable(table, mapping); err != nil {
		return err
	}

	name, err := n.syncer.EnsureMapping(table, mapping)
	if err != nil {
		return err
	}

	// This shouldn't take long and is not critical to the upload.
	viewCtx, viewCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer viewCancel()

	if err := n.syncer.EnsureView(viewCtx, table); err != nil {
		logger.Errorf("Failed to create view %s.%s: %v", database, table, err)
	}

	n.mu.RLock()
	ingestor := n.ingestors[table]
	n.mu.RUnlock()

	if ingestor == nil {
		ingestor, err = func() (ingest.Ingestor, error) {
			n.mu.Lock()
			defer n.mu.Unlock()

			ingestor = n.ingestors[table]
			if ingestor != nil {
				return ingestor, nil
			}

			if n.opts.Kustainer {
				ingestor = testutils.NewUploadReader(n.KustoCli, database, table)
				n.ingestors[table] = ingestor
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
		return sanitizeErrorString(err)
	}

	err = <-res.Wait(ctx)
	if err != nil {
		return err
	}

	return nil
}

func sanitizeErrorString(err error) error {
	errString := err.Error()
	r := regexp.MustCompile(`sig=[a-zA-Z0-9]+`)
	errString = r.ReplaceAllString(errString, "sig=REDACTED")
	return fmt.Errorf(errString)
}

func (n *uploader) upload(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case batch := <-n.queue:
			segments := batch.Segments

			if batch.Database != n.database {
				logger.Errorf("Database mismatch: %s != %s. Skipping batch", batch.Database, n.database)
				continue
			}

			func() {
				defer batch.Release()

				var (
					readers        = make([]io.Reader, 0, len(segments))
					segmentReaders = make([]*wal.SegmentReader, 0, len(segments))
					database       string
					table          string
					schema         string
					header         string
					err            error
				)

				for _, si := range segments {
					database, table, schema, _, err = wal.ParseFilename(si.Path)
					if err != nil {
						logger.Errorf("Failed to parse file: %s", err.Error())
						continue
					}
					metrics.SampleLatency.WithLabelValues(database, table).Set(time.Since(si.CreatedAt).Seconds())

					var opts []wal.Option
					if schema != "" {
						opts = append(opts, wal.WithSkipHeader)
					}
					f, err := wal.NewSegmentReader(si.Path, opts...)
					if os.IsNotExist(err) {
						// batches are not disjoint, so the same segments could be included in multiple batches.
						continue
					} else if err != nil {
						logger.Errorf("Failed to open file: %s", err.Error())
						continue
					}

					segmentReaders = append(segmentReaders, f)
					readers = append(readers, f)
				}

				defer func(segmentReaders []*wal.SegmentReader) {
					for _, sr := range segmentReaders {
						sr.Close()
					}
				}(segmentReaders)

				if len(segmentReaders) == 0 {
					return
				}

				samplePath := segments[0].Path
				database, table, schema, _, err = wal.ParseFilename(samplePath)
				if err != nil {
					logger.Errorf("Failed to parse file: %s", err.Error())
					return
				}

				mapping := n.opts.DefaultMapping
				if schema != "" {
					header, err = n.extractSchema(samplePath)
					if err != nil {
						logger.Errorf("Failed to extract schema: %s", err.Error())
						return
					}

					mapping, err = adxschema.UnmarshalSchema(header)
					if err != nil {
						logger.Errorf("Failed to unmarshal schema: %s", err.Error())
						return
					}
				}

				mr := io.MultiReader(readers...)

				now := time.Now()
				if err := n.uploadReader(mr, database, table, mapping); err != nil {
					logger.Errorf("Failed to upload file: %s", err.Error())
					return
				}

				if logger.IsDebug() {
					logger.Debugf("Uploaded %v duration=%s", segments, time.Since(now).String())
				}

				if err := batch.Remove(); err != nil {
					logger.Errorf("Failed to remove batch: %s", err.Error())
				}

				for _, sr := range segmentReaders {
					sampleType, sampleCount := sr.SampleMetadata()

					switch sampleType {
					case wal.MetricSampleType:
						metrics.MetricsUploaded.WithLabelValues(database, table).Add(float64(sampleCount))
					case wal.LogSampleType:
						metrics.LogsUploaded.WithLabelValues(database, table).Add(float64(sampleCount))
					}
				}
			}()

		}
	}
}

func (n *uploader) extractSchema(path string) (string, error) {
	f, err := wal.NewSegmentReader(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	b := make([]byte, 4096)
	nn, err := f.Read(b)
	if err != nil {
		return "", err
	}
	b = b[:nn]

	idx := bytes.IndexByte(b, '\n')
	if idx != -1 {
		return string(b[:idx]), nil
	}
	return string(b), nil
}
