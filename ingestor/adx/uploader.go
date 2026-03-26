package adx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/service"
	"github.com/Azure/adx-mon/pkg/wal"
	adxschema "github.com/Azure/adx-mon/schema"
	azkustodata "github.com/Azure/azure-kusto-go/azkustodata"
	"github.com/Azure/azure-kusto-go/azkustodata/kql"
	kustov1 "github.com/Azure/azure-kusto-go/azkustodata/query/v1"
	azkustoingest "github.com/Azure/azure-kusto-go/azkustoingest"
)

const ConcurrentUploads = 50

type Uploader interface {
	service.Component

	Database() string
	Endpoint() string

	// UploadQueue returns a channel that can be used to upload files to kusto.
	UploadQueue() chan *cluster.Batch

	// Mgmt executes a management query against the database.
	Mgmt(ctx context.Context, query azkustodata.Statement, options ...azkustodata.QueryOption) (kustov1.Dataset, error)
}

type uploader struct {
	KustoCli   *azkustodata.Client
	storageDir string
	database   string
	opts       UploaderOpts
	syncer     *Syncer

	queue   chan *cluster.Batch
	closeFn context.CancelFunc

	wg                  sync.WaitGroup
	mu                  sync.RWMutex
	ingestor            *azkustoingest.Ingestion
	requireDirectIngest bool
}

type UploaderOpts struct {
	StorageDir        string
	Database          string
	ConcurrentUploads int
	Dimensions        []string
	DefaultMapping    adxschema.SchemaMapping
	SampleType        SampleType
}

func NewUploader(kustoCli *azkustodata.Client, opts UploaderOpts) *uploader {
	syncer := NewSyncer(kustoCli, opts.Database, opts.DefaultMapping, opts.SampleType)

	return &uploader{
		KustoCli:   kustoCli,
		syncer:     syncer,
		storageDir: opts.StorageDir,
		database:   opts.Database,
		opts:       opts,
		queue:      make(chan *cluster.Batch, 10000),
	}
}

func (n *uploader) Open(ctx context.Context) error {
	c, closeFn := context.WithCancel(ctx)
	n.closeFn = closeFn

	if err := n.syncer.Open(c); err != nil {
		return err
	}

	requireDirectIngest, err := n.clusterRequiresDirectIngest(ctx)
	if err != nil {
		return err
	}
	if requireDirectIngest {
		logger.Warnf("Cluster=%s requires direct ingest: %s", n.database, n.KustoCli.Endpoint())
		n.requireDirectIngest = true
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

	if n.ingestor != nil {
		n.ingestor.Close()
	}

	n.ingestor = nil
	return n.syncer.Close()
}

func (n *uploader) UploadQueue() chan *cluster.Batch {
	return n.queue
}

func (n *uploader) Database() string {
	return n.database
}

func (n *uploader) Endpoint() string {
	return n.KustoCli.Endpoint()
}

func (n *uploader) Mgmt(ctx context.Context, query azkustodata.Statement, options ...azkustodata.QueryOption) (kustov1.Dataset, error) {
	return n.KustoCli.Mgmt(ctx, n.database, query, options...)
}

func (n *uploader) uploadReader(reader io.Reader, database, table string, mapping adxschema.SchemaMapping) error {
	// Ensure we wait for this upload to finish.
	n.wg.Add(1)
	defer n.wg.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if err := n.syncer.EnsureTable(table, mapping); err != nil {
		return err
	}

	name, err := n.syncer.EnsureMapping(table, mapping)
	if err != nil {
		return err
	}

	if n.requireDirectIngest {
		return n.directIngestReader(ctx, reader, table)
	}

	n.mu.RLock()
	ingestor := n.ingestor
	n.mu.RUnlock()

	if ingestor == nil {
		ingestor, err = func() (*azkustoingest.Ingestion, error) {
			n.mu.Lock()
			defer n.mu.Unlock()

			ingestor = n.ingestor
			if ingestor != nil {
				return ingestor, nil
			}

			kcsb := azkustodata.NewConnectionStringBuilder(n.KustoCli.Endpoint())
			if strings.HasPrefix(n.KustoCli.Endpoint(), "https://") {
				kcsb.WithDefaultAzureCredential()
			}

			ingestor, err = azkustoingest.New(kcsb,
				azkustoingest.WithDefaultDatabase(n.database),
			)
			if err != nil {
				return nil, err
			}
			n.ingestor = ingestor
			return ingestor, nil
		}()

		if err != nil {
			return err
		}
	}

	// uploadReader our file WITHOUT status reporting.
	// When completed, delete the file on local storage we are uploading.
	res, err := ingestor.FromReader(ctx, reader,
		azkustoingest.Table(table),
		azkustoingest.IngestionMappingRef(name, azkustoingest.CSV),
	)
	if err != nil {
		return sanitizeErrorString(err)
	}

	err = <-res.Wait(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (n *uploader) directIngestReader(ctx context.Context, reader io.Reader, table string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read data: %w", err)
	}

	stmt := kql.New(".ingest inline into table ").AddTable(table).AddLiteral(" <| ").AddUnsafe(string(data))
	_, err = n.KustoCli.Mgmt(ctx, n.database, stmt)
	if err != nil {
		return fmt.Errorf("failed to ingest data: %w", err)
	}

	return nil
}

func sanitizeErrorString(err error) error {
	errString := err.Error()
	r := regexp.MustCompile(`sig=[a-zA-Z0-9]+`)
	errString = r.ReplaceAllString(errString, "sig=REDACTED")
	return errors.New(errString)
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
				batch.Release()
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
					if err := batch.Remove(); err != nil {
						logger.Errorf("Failed to remove batch: %s", err.Error())
					}
					return
				}

				samplePath := segmentReaders[0].Path()
				database, table, schema, _, err = wal.ParseFilename(samplePath)
				if err != nil {
					logger.Errorf("Failed to parse file: %s: %s", samplePath, err.Error())
					return
				}

				mapping := n.opts.DefaultMapping
				if schema != "" {
					header, err = n.extractSchema(samplePath)
					if err != nil {
						logger.Errorf("Failed to extract schema: %s: %s", samplePath, err.Error())

						// This batch is invalid, remove it.
						if err := batch.Remove(); err != nil {
							logger.Errorf("Failed to remove batch: %s", err.Error())
						}

						return
					}

					mapping, err = adxschema.UnmarshalSchema(header)
					if err != nil {
						logger.Errorf("Failed to unmarshal schema: %s: %s", samplePath, err.Error())

						// This batch is invalid, remove it.
						if err := batch.Remove(); err != nil {
							logger.Errorf("Failed to remove batch: %s", err.Error())
						}
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

				metrics.IngestorSegmentsUploadedTotal.WithLabelValues(batch.Prefix).Add(float64(len(segmentReaders)))
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

// clusterRequiresDirectIngest checks if the cluster is configured to require direct ingest.
// In particular, if a cluster's details have a named marked as KustoPersonal, we know this
// cluster to be a Kustainer, which does not support queued or streaming ingestion.
// https://learn.microsoft.com/en-us/azure/data-explorer/kusto-emulator-overview#limitations
func (n *uploader) clusterRequiresDirectIngest(ctx context.Context) (bool, error) {
	stmt := kql.New(".show cluster details")
	ds, err := n.KustoCli.Mgmt(ctx, n.database, stmt)
	if err != nil {
		return false, fmt.Errorf("failed to query cluster details: %w", err)
	}
	table, ok := primaryResultTable(ds)
	if !ok {
		return false, fmt.Errorf("failed to query cluster details: missing primary result table")
	}

	for _, row := range table.Rows() {
		var cs clusterDetails
		if err := row.ToStruct(&cs); err != nil {
			return false, fmt.Errorf("failed to convert row to struct: %w", err)
		}
		return cs.Name == "KustoPersonal", nil
	}
	return false, nil
}

type clusterDetails struct {
	NodeId                 string    `kusto:"NodeId"`
	Address                string    `kusto:"Address"`
	Name                   string    `kusto:"Name"`
	StartTime              time.Time `kusto:"StartTime"`
	AssignedHotExtents     int       `kusto:"AssignedHotExtents"`
	IsAdmin                bool      `kusto:"IsAdmin"`
	MachineTotalMemory     int64     `kusto:"MachineTotalMemory"`
	MachineAvailableMemory int64     `kusto:"MachineAvailableMemory"`
	ProcessorCount         int       `kusto:"ProcessorCount"`
	HotExtentsOriginalSize int64     `kusto:"HotExtentsOriginalSize"`
	HotExtentsSize         int64     `kusto:"HotExtentsSize"`
	EnvironmentDescription string    `kusto:"EnvironmentDescription"`
	ProductVersion         string    `kusto:"ProductVersion"`
	Reserved0              int       `kusto:"Reserved0"`
	ClockDescription       string    `kusto:"ClockDescription"`
	RuntimeDescription     string    `kusto:"RuntimeDescription"`
}
