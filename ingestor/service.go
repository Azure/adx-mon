package ingestor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/adx-mon/ingestor/adx"
	"github.com/Azure/adx-mon/ingestor/cluster"
	ingestorstorage "github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/debug"
	adxhttp "github.com/Azure/adx-mon/pkg/http"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/reader"
	"github.com/Azure/adx-mon/pkg/scheduler"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/storage"
	"github.com/klauspost/compress/gzip"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// invalidEntityCharacters is a regex that matches invalid characters for Kusto entities and segment files.
// This is a subset of the invalid characters for Kusto entities and segment files naming patterns.  This should
// match tranform.Normalize.
var invalidEntityCharacters = regexp.MustCompile(`[^a-zA-Z0-9]`)

type Interface interface {
	Open(ctx context.Context) error
	Close() error
	HandleReady(w http.ResponseWriter, r *http.Request)
	HandleTransfer(w http.ResponseWriter, r *http.Request)
	Shutdown(ctx context.Context) error
	UploadSegments(ctx context.Context) error
	DisableWrites() error
}

type Service struct {
	walOpts wal.WALOpts
	opts    ServiceOpts

	// database is a map of known DB names used for validating requests.
	databases map[string]struct{}

	uploader    adx.Uploader
	replicator  cluster.Replicator
	coordinator cluster.Coordinator
	batcher     cluster.Batcher
	closeFn     context.CancelFunc

	store   storage.Store
	metrics metrics.Service

	scheduler *scheduler.Periodic

	dropFilePrefixes []string
	health           interface{ IsHealthy() bool }
}

type ServiceOpts struct {
	StorageDir     string
	Uploader       adx.Uploader
	MaxSegmentSize int64
	MaxSegmentAge  time.Duration

	K8sCli     kubernetes.Interface
	K8sCtrlCli client.Client

	// MetricsKustoCli is the Kusto client connected to the metrics kusto cluster.
	MetricsKustoCli []metrics.StatementExecutor

	// LogsKustoCli is the Kusto client connected to the logs kusto cluster.
	LogsKustoCli []metrics.StatementExecutor

	// InsecureSkipVerify disables TLS certificate verification.
	InsecureSkipVerify bool

	// Namespace is the namespace used for peer discovery.
	Namespace string

	// Hostname is the hostname of the current node.
	Hostname string

	// Region is a location identifier
	Region string

	// DisablePeerTransfer disables peer discovery and prevents transfers of small segments to an owner.
	// Each instance of ingestor will upload received segments directly.
	DisablePeerTransfer bool

	// MaxTransferSize is the minimum size of a segment that will be transferred to another node.  If a segment
	// exceeds this size, it will be uploaded directly by the current node.
	MaxTransferSize int64

	// MaxTransferAge is the maximum age of a segment that will be transferred to another node.  If a segment
	// exceeds this age, it will be uploaded directly by the current node.
	MaxTransferAge time.Duration

	// MaxSegmentCount is the maximum number of segments files allowed on disk before signaling back-pressure.
	MaxSegmentCount int64

	// MaxDiskUsage is the maximum disk usage allowed before signaling back-pressure.
	MaxDiskUsage int64

	// AllowedDatabases is the distinct set of database names that are allowed to be written to.
	AllowedDatabase []string

	// MetricsDatabase is the name of the metrics database.
	MetricsDatabases []string

	// LogsDatabases is a slice of log database names.
	LogsDatabases []string

	// PartitionSize is the max size of the group of nodes forming a partition.  A partition is a set of nodes where
	// keys are distributed.
	PartitionSize int

	// MaxTransferConcurrency is the maximum number of concurrent transfers allowed in flight at the same time.
	MaxTransferConcurrency int

	// EnableWALFsync enables fsync of segments before closing the segment.
	EnableWALFsync bool

	// DropFilePrefixes is a slice of prefixes that will be dropped when importing segments.
	DropFilePrefixes []string

	// MaxBatchSegments is the maximum number of segments to include when transferring segments in a batch.  The segments
	// are merged into a new segment.  A higher number takes longer to combine on the sending side and increases the
	// size of segments on the receiving side.  A lower number creates more batches and high remote transfer calls.  If
	// not specified, the default is 25.
	MaxBatchSegments int

	// SlowRequestThreshold is the threshold for logging slow requests.
	SlowRequestThreshold float64

	ClusterLabels map[string]string
}

func NewService(opts ServiceOpts) (*Service, error) {
	store := storage.NewLocalStore(storage.StoreOpts{
		StorageDir:     opts.StorageDir,
		SegmentMaxSize: opts.MaxSegmentSize,
		SegmentMaxAge:  opts.MaxSegmentAge,
		EnableWALFsync: opts.EnableWALFsync,
	})

	coord, err := cluster.NewCoordinator(&cluster.CoordinatorOpts{
		WriteTimeSeriesFn: store.WriteTimeSeries,
		K8sCli:            opts.K8sCli,
		Hostname:          opts.Hostname,
		Namespace:         opts.Namespace,
		PartitionSize:     opts.PartitionSize,
	})
	if err != nil {
		return nil, err
	}

	health := cluster.NewHealth(cluster.HealthOpts{
		UnhealthyTimeout: time.Minute,
		MaxSegmentCount:  opts.MaxSegmentCount,
		MaxDiskUsage:     opts.MaxDiskUsage,
	})

	repl, err := cluster.NewReplicator(cluster.ReplicatorOpts{
		Hostname:               opts.Hostname,
		Partitioner:            coord,
		InsecureSkipVerify:     opts.InsecureSkipVerify,
		Health:                 health,
		SegmentRemover:         store,
		MaxTransferConcurrency: opts.MaxTransferConcurrency,
		DisableGzip:            true,
	})
	if err != nil {
		return nil, err
	}

	batcher := cluster.NewBatcher(cluster.BatcherOpts{
		StorageDir:         opts.StorageDir,
		MaxSegmentAge:      opts.MaxSegmentAge,
		MaxTransferSize:    opts.MaxTransferSize,
		MaxTransferAge:     opts.MaxTransferAge,
		MaxBatchSegments:   opts.MaxBatchSegments,
		Partitioner:        coord,
		Segmenter:          store.Index(),
		UploadQueue:        opts.Uploader.UploadQueue(),
		TransferQueue:      repl.TransferQueue(),
		PeerHealthReporter: health,
		TransfersDisabled:  opts.DisablePeerTransfer,
	})

	health.QueueSizer = batcher

	allKustoCli := make([]metrics.StatementExecutor, 0, len(opts.MetricsKustoCli)+len(opts.LogsKustoCli))
	allKustoCli = append(allKustoCli, opts.MetricsKustoCli...)
	allKustoCli = append(allKustoCli, opts.LogsKustoCli...)

	metricsSvc := metrics.NewService(metrics.ServiceOpts{
		Hostname:         opts.Hostname,
		Elector:          coord,
		MetricsKustoCli:  opts.MetricsKustoCli,
		KustoCli:         allKustoCli,
		PeerHealthReport: health,
	})

	dbs := make(map[string]struct{}, len(opts.AllowedDatabase))
	for _, db := range opts.AllowedDatabase {
		dbs[db] = struct{}{}
	}

	databases := make(map[string]struct{})
	for _, db := range opts.LogsDatabases {
		databases[db] = struct{}{}
	}
	for _, db := range opts.MetricsDatabases {
		databases[db] = struct{}{}
	}

	sched := scheduler.NewScheduler(coord)

	return &Service{
		opts:             opts,
		databases:        databases,
		uploader:         opts.Uploader,
		replicator:       repl,
		store:            store,
		coordinator:      coord,
		batcher:          batcher,
		metrics:          metricsSvc,
		health:           health,
		scheduler:        sched,
		dropFilePrefixes: opts.DropFilePrefixes,
	}, nil
}

func (s *Service) Open(ctx context.Context) error {
	var svcCtx context.Context
	svcCtx, s.closeFn = context.WithCancel(ctx)

	if err := s.store.Open(svcCtx); err != nil {
		return err
	}

	if err := s.coordinator.Open(svcCtx); err != nil {
		return err
	}

	if err := s.batcher.Open(svcCtx); err != nil {
		return err
	}

	if err := s.replicator.Open(svcCtx); err != nil {
		return err
	}

	if err := s.metrics.Open(svcCtx); err != nil {
		return err
	}

	if err := s.scheduler.Open(svcCtx); err != nil {
		return err
	}

	s.scheduler.ScheduleEvery(time.Minute, "ingestor-health-check", func(ctx context.Context) error {
		metrics.IngestorHealthCheck.WithLabelValues(s.opts.Region).Set(1)
		return nil
	})

	fnStore := ingestorstorage.NewFunctions(s.opts.K8sCtrlCli, s.coordinator)
	crdStore := ingestorstorage.NewCRDHandler(s.opts.K8sCtrlCli, s.coordinator)
	for _, v := range s.opts.MetricsKustoCli {
		t := adx.NewDropUnusedTablesTask(v)
		s.scheduler.ScheduleEvery(12*time.Hour, "delete-unused-tables", func(ctx context.Context) error {
			return t.Run(ctx)
		})

		f := adx.NewSyncFunctionsTask(fnStore, v)
		s.scheduler.ScheduleEvery(time.Minute, "sync-metrics-functions", func(ctx context.Context) error {
			return f.Run(ctx)
		})

		m := adx.NewManagementCommandsTask(crdStore, v)
		s.scheduler.ScheduleEvery(10*time.Minute, "management-commands", func(ctx context.Context) error {
			return m.Run(ctx)
		})

		sr := adx.NewSummaryRuleTask(crdStore, v, s.opts.ClusterLabels)
		s.scheduler.ScheduleEvery(time.Minute, "summary-rules", func(ctx context.Context) error {
			return sr.Run(ctx)
		})
	}

	for _, v := range s.opts.LogsKustoCli {
		f := adx.NewSyncFunctionsTask(fnStore, v)
		s.scheduler.ScheduleEvery(time.Minute, "sync-logs-functions", func(ctx context.Context) error {
			return f.Run(ctx)
		})

		m := adx.NewManagementCommandsTask(crdStore, v)
		s.scheduler.ScheduleEvery(10*time.Minute, "management-commands", func(ctx context.Context) error {
			return m.Run(ctx)
		})

		sr := adx.NewSummaryRuleTask(crdStore, v, s.opts.ClusterLabels)
		s.scheduler.ScheduleEvery(time.Minute, "summary-rules", func(ctx context.Context) error {
			return sr.Run(ctx)
		})
	}

	t := adx.NewAuditDiskSpaceTask(s.batcher, s.opts.StorageDir)
	s.scheduler.ScheduleEvery(5*time.Minute, "audit-disk-space", func(ctx context.Context) error {
		return t.Run(ctx)
	})

	return nil
}

func (s *Service) Close() error {
	s.closeFn()

	if err := s.scheduler.Close(); err != nil {
		return err
	}

	if err := s.metrics.Close(); err != nil {
		return err
	}

	if err := s.replicator.Close(); err != nil {
		return err
	}

	if err := s.batcher.Close(); err != nil {
		return err
	}

	if err := s.coordinator.Close(); err != nil {
		return err
	}

	return s.store.Close()
}

func (s *Service) HandleDebugStore(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	if debugWriter, ok := s.store.(debug.DebugWriter); ok {
		if err := debugWriter.WriteDebug(w); err != nil {
			logger.Errorf("Failed to write debug info: %s", err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		return
	}
}

// HandleReady handles the readiness probe.
func (s *Service) HandleReady(w http.ResponseWriter, r *http.Request) {
	if s.health.IsHealthy() {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	w.Write([]byte("not ready"))
}

// HandleTransfer handles the transfer WAL segments from other nodes in the cluster.
func (s *Service) HandleTransfer(w http.ResponseWriter, r *http.Request) {
	adxhttp.MaybeCloseConnection(w, r)

	start := time.Now()
	m := metrics.RequestsReceived.MustCurryWith(prometheus.Labels{"path": "/transfer"})
	filename := r.URL.Query().Get("filename")
	if filename == "" {
		m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
		http.Error(w, "missing filename", http.StatusBadRequest)
		return
	}

	xff := r.Header.Get("X-Forwarded-For")
	// If the header is present, split it by comma and take the first IP address
	var originalIP string
	if xff != "" {
		ips := strings.Split(xff, ",")
		originalIP = strings.TrimSpace(ips[0])
	} else {
		// If the header is not present, use the remote address
		originalIP = r.RemoteAddr
	}

	cr := reader.NewCounterReader(r.Body)
	var body io.ReadCloser = cr
	defer func() {
		io.Copy(io.Discard, body)

		metrics.RequestsBytesReceived.Add(float64(cr.Count()))
		dur := time.Since(start)
		if s.opts.SlowRequestThreshold > 0 && dur.Seconds() > s.opts.SlowRequestThreshold {
			logger.Warnf("Slow request: path=/transfer duration=%s from=%s size=%d file=%s", dur.String(), originalIP, cr.Count(), filename)
		}
		if err := body.Close(); err != nil {
			logger.Errorf("Close http body: %s, path=/transfer duration=%s from=%s", err.Error(), dur.String(), originalIP)
		}
	}()

	for _, prefix := range s.dropFilePrefixes {
		if strings.HasPrefix(filename, prefix) {
			io.Copy(io.Discard, body)
			metrics.IngestorDroppedPrefixes.WithLabelValues(prefix).Inc()
			m.WithLabelValues(strconv.Itoa(http.StatusAccepted)).Inc()
			w.WriteHeader(http.StatusAccepted)
			return
		}
	}

	if !s.health.IsHealthy() {
		m.WithLabelValues(strconv.Itoa(http.StatusTooManyRequests)).Inc()
		http.Error(w, "Overloaded. Retry later", http.StatusTooManyRequests)
		return
	}

	// https://pkg.go.dev/io/fs#ValidPath
	// Check for possible traversal attacks.
	f := s.validateFileName(filename)
	if f == "" {
		logger.Errorf("Transfer requested with an invalid filename %q", filename)
		m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
		http.Error(w, "Filename is invalid", http.StatusBadRequest)
		return
	}

	// If the request is gzipped, create a gzip reader to decompress the body.
	if r.Header.Get("Content-Encoding") == "gzip" {
		gzipReader, err := gzip.NewReader(body)
		if err != nil {
			logger.Errorf("Failed to create gzip reader: %s", err.Error())
			m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
			http.Error(w, "Invalid gzip encoding", http.StatusBadRequest)
			return
		}
		defer gzipReader.Close()
		body = gzipReader
	}

	n, err := s.store.Import(f, body)
	if errors.Is(err, wal.ErrMaxSegmentsExceeded) || errors.Is(err, wal.ErrMaxDiskUsageExceeded) {
		m.WithLabelValues(strconv.Itoa(http.StatusTooManyRequests)).Inc()
		http.Error(w, "Overloaded. Retry later", http.StatusTooManyRequests)
		return
	} else if errors.Is(err, wal.ErrSegmentLocked) {
		http.Error(w, err.Error(), http.StatusLocked)
		return
	} else if err != nil && strings.Contains(err.Error(), "block checksum verification failed") {
		logger.Errorf("Transfer requested with checksum error %q from=%s", filename, originalIP)
		m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
		http.Error(w, "Block checksum verification failed", http.StatusBadRequest)
		return
	} else if err != nil {
		logger.Errorf("Failed to import %s: %s from=%s", filename, err.Error(), originalIP)
		m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	} else {
		if logger.IsDebug() {
			logger.Debugf("Imported %d bytes to %s", n, filename)
		}
	}
	m.WithLabelValues(strconv.Itoa(http.StatusAccepted)).Inc()
	w.WriteHeader(http.StatusAccepted)
}

func (s *Service) Shutdown(ctx context.Context) error {
	if err := s.metrics.Close(); err != nil {
		return err
	}

	if err := s.UploadSegments(ctx); err != nil {
		return fmt.Errorf("Failed to upload segments: %s", err.Error())
	}

	return nil
}

func (s *Service) UploadSegments(ctx context.Context) error {
	if err := s.batcher.BatchSegments(); err != nil {
		return err
	}
	logger.Infof("Waiting for upload queue to drain, %d batches remaining", len(s.uploader.UploadQueue()))
	logger.Infof("Waiting for transfer queue to drain, %d batches remaining", len(s.replicator.TransferQueue()))

	t := time.NewTicker(time.Second)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			if len(s.uploader.UploadQueue()) == 0 && len(s.replicator.TransferQueue()) == 0 {
				return nil
			}

			if len(s.uploader.UploadQueue()) != 0 {
				logger.Infof("Waiting for upload queue to drain, %d batches remaining", len(s.uploader.UploadQueue()))
			}
			if len(s.replicator.TransferQueue()) != 0 {
				logger.Infof("Waiting for transfer queue to drain, %d batches remaining", len(s.replicator.TransferQueue()))
			}
		case <-ctx.Done():
			return fmt.Errorf("timed out to upload segments")
		}
	}
}

func (s *Service) DisableWrites() error {
	if err := s.metrics.Close(); err != nil {
		return err
	}

	if err := s.store.Close(); err != nil {
		return err
	}
	return nil
}

func (s *Service) validateFileName(filename string) string {
	if !fs.ValidPath(filename) {
		return ""
	}

	db, table, schema, epoch, err := wal.ParseFilename(filename)
	if err != nil {
		return ""
	}

	if invalidEntityCharacters.MatchString(db) || invalidEntityCharacters.MatchString(table) || invalidEntityCharacters.MatchString(epoch) || invalidEntityCharacters.MatchString(schema) {
		return ""
	}

	if _, ok := s.databases[db]; !ok {
		return ""
	}

	return wal.Filename(db, table, schema, epoch)
}
