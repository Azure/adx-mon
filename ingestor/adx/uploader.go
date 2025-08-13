package adx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	kustoerrors "github.com/Azure/azure-kusto-go/kusto/data/errors"
	"github.com/Azure/azure-kusto-go/kusto/ingest"
	"github.com/Azure/azure-kusto-go/kusto/kql"
)

const ConcurrentUploads = 50

type Uploader interface {
	service.Component

	Database() string
	Endpoint() string

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

	wg                  sync.WaitGroup
	mu                  sync.RWMutex
	ingestors           map[string]ingest.Ingestor
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

// UploadError wraps an error from Kusto ingestion, carrying an HTTPStatus when available.
type UploadError struct {
	Err        error
	HTTPStatus int
	// DeadletterHint indicates we should capture the batch payload even if HTTP status is unknown.
	DeadletterHint bool
}

func (e *UploadError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}
func (e *UploadError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewUploader(kustoCli *kusto.Client, opts UploaderOpts) *uploader {
	syncer := NewSyncer(kustoCli, opts.Database, opts.DefaultMapping, opts.SampleType)

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

func (n *uploader) Endpoint() string {
	return n.KustoCli.Endpoint()
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

			if n.requireDirectIngest {
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
		// Preserve HTTP status (e.g., 400) for caller while sanitizing message for logs.
		status := 0
		var httpErr *kustoerrors.HttpError
		if errors.As(err, &httpErr) {
			status = httpErr.StatusCode
		}
		// Detect sentinel blob upload failures from the queued ingest path.
		deadletter := false
		var ke *kustoerrors.Error
		if errors.As(err, &ke) {
			if ke.Op == kustoerrors.OpFileIngest && ke.Kind == kustoerrors.KBlobstore && strings.Contains(ke.Error(), "problem uploading to Blob Storage") {
				deadletter = true
			}
		}
		return &UploadError{Err: sanitizeErrorString(err), HTTPStatus: status, DeadletterHint: deadletter}
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
	return errors.New(errString)
}

// tryDeadletterStream attempts to capture the full batch payload that produced an HTTP 400.
// It creates storageDir/"deadletter" exclusively; if it already exists, it returns immediately.
// Best-effort: any failures are logged and ignored.
func (n *uploader) tryDeadletterStream(segmentPaths []string, skipHeader bool, database, table string, cause error) {
	// Single, fixed file in storage dir acts as occupancy gate.
	dstPath := filepath.Join(n.storageDir, "deadletter")
	// O_EXCL prevents races across goroutines/processes.
	f, err := os.OpenFile(dstPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		// If it already exists, someone else captured; that's fine.
		if !os.IsExist(err) {
			if logger.IsDebug() {
				logger.Debugf("deadletter: failed to create file: %s", err.Error())
			}
		}
		return
	}

	// Ensure the file gets closed.
	defer func() {
		// Best-effort fsync to persist the captured payload.
		_ = f.Sync()
		_ = f.Close()
	}()

	// Copy all segments in order, reconstructing the original MultiReader body.
	buf := make([]byte, 64*1024)
	for _, p := range segmentPaths {
		var opts []wal.Option
		if skipHeader {
			opts = append(opts, wal.WithSkipHeader)
		}
		sr, err := wal.NewSegmentReader(p, opts...)
		if err != nil {
			if logger.IsDebug() {
				logger.Debugf("deadletter: failed to open segment %s: %s", p, err.Error())
			}
			return
		}
		// Ensure each reader is closed
		func() {
			defer sr.Close()
			if _, err := io.CopyBuffer(f, sr, buf); err != nil {
				if logger.IsDebug() {
					logger.Debugf("deadletter: failed to copy segment %s: %s", p, err.Error())
				}
			}
		}()
	}

	// Log minimal sanitized context for operators
	if logger.IsDebug() {
		logger.Debugf("deadletter: captured payload for %s.%s to %s: %v", database, table, dstPath, cause)
	} else {
		logger.Warnf("deadletter: captured payload for %s.%s to %s", database, table, dstPath)
	}
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
					// If Kusto returned HTTP 400 for this batch, capture the full payload once.
					var ue *UploadError
					if errors.As(err, &ue) && ue.HTTPStatus == 400 {
						// Build ordered paths and header-skip flag to reconstruct the stream.
						paths := make([]string, 0, len(segmentReaders))
						for _, sr := range segmentReaders {
							paths = append(paths, sr.Path())
						}
						n.tryDeadletterStream(paths, schema != "", database, table, err)
					} else if errors.As(err, &ue) && ue.DeadletterHint {
						paths := make([]string, 0, len(segmentReaders))
						for _, sr := range segmentReaders {
							paths = append(paths, sr.Path())
						}
						n.tryDeadletterStream(paths, schema != "", database, table, err)
					} else {
						// Also capture on direct Kusto HTTP errors with status 400 (e.g., mgmt/streaming paths),
						// and on permanent ingestion failures returned by res.Wait().
						var httpErr *kustoerrors.HttpError
						if errors.As(err, &httpErr) && httpErr.StatusCode == 400 {
							paths := make([]string, 0, len(segmentReaders))
							for _, sr := range segmentReaders {
								paths = append(paths, sr.Path())
							}
							n.tryDeadletterStream(paths, schema != "", database, table, err)
						} else if ingest.IsStatusRecord(err) {
							if fs, getErr := ingest.GetIngestionFailureStatus(err); getErr == nil && fs == ingest.Permanent {
								paths := make([]string, 0, len(segmentReaders))
								for _, sr := range segmentReaders {
									paths = append(paths, sr.Path())
								}
								n.tryDeadletterStream(paths, schema != "", database, table, err)
							}
						}
					}
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
	rows, err := n.KustoCli.Mgmt(ctx, n.database, stmt)
	if err != nil {
		return false, fmt.Errorf("failed to query cluster details: %w", err)
	}
	defer rows.Stop()

	for {
		row, errInline, errFinal := rows.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			continue
		}
		if errFinal != nil {
			return false, fmt.Errorf("failed to retrieve cluster details: %w", errFinal)
		}

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
