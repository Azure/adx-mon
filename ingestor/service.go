package ingestor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/logs/v1/logsv1connect"
	"github.com/Azure/adx-mon/ingestor/adx"
	"github.com/Azure/adx-mon/ingestor/cluster"
	metricsHandler "github.com/Azure/adx-mon/ingestor/metrics"
	"github.com/Azure/adx-mon/ingestor/otlp"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/ingestor/transform"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/kubernetes"
)

// invalidEntityCharacters is a regex that matches invalid characters for Kusto entities and segment files.
// This is a subset of the invalid characters for Kusto entities and segment files naming patterns.  This should
// match tranform.Normalize.
var invalidEntityCharacters = regexp.MustCompile(`[^a-zA-Z0-9]`)

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

	handler       *metricsHandler.Handler
	logsHandler   http.Handler
	requestFilter *transform.RequestTransformer
	health        interface{ IsHealthy() bool }
}

type ServiceOpts struct {
	StorageDir     string
	Uploader       adx.Uploader
	MaxSegmentSize int64
	MaxSegmentAge  time.Duration

	// StaticColumns is a slice of column=value elements where each element will be added all rows.
	StaticColumns []string

	// LiftedColumns is a slice of label names where each element will be added as a column if the label exists.
	LiftedColumns []string

	// DropLabels is a map of metric names regexes to label name regexes.  When both match, the label will be dropped.
	DropLabels map[*regexp.Regexp]*regexp.Regexp

	// DropMetrics is a slice of regexes that drops metrics when the metric name matches.  The metric name format
	// should match the Prometheus naming style before the metric is translated to a Kusto table name.
	DropMetrics []*regexp.Regexp

	K8sCli kubernetes.Interface

	// MetricsKustoCli is the Kusto client connected to the metrics kusto cluster.
	MetricsKustoCli []metrics.StatementExecutor

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
}

func NewService(opts ServiceOpts) (*Service, error) {
	store := storage.NewLocalStore(storage.StoreOpts{
		StorageDir:     opts.StorageDir,
		SegmentMaxSize: opts.MaxSegmentSize,
		SegmentMaxAge:  opts.MaxSegmentAge,
		LiftedColumns:  opts.LiftedColumns,
	})

	coord, err := cluster.NewCoordinator(&cluster.CoordinatorOpts{
		WriteTimeSeriesFn: store.WriteTimeSeries,
		WriteOTLPLogsFn:   store.WriteOTLPLogs,
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
		Hostname:           opts.Hostname,
		Partitioner:        coord,
		InsecureSkipVerify: opts.InsecureSkipVerify,
		Health:             health,
		SegmentRemover:     store,
	})
	if err != nil {
		return nil, err
	}

	batcher := cluster.NewBatcher(cluster.BatcherOpts{
		StorageDir:         opts.StorageDir,
		MaxSegmentAge:      opts.MaxSegmentAge,
		MaxTransferSize:    opts.MaxTransferSize,
		MaxTransferAge:     opts.MaxTransferAge,
		Partitioner:        coord,
		Segmenter:          store.Index(),
		UploadQueue:        opts.Uploader.UploadQueue(),
		TransferQueue:      repl.TransferQueue(),
		PeerHealthReporter: health,
		TransfersDisabled:  opts.DisablePeerTransfer,
	})

	health.QueueSizer = batcher

	metricsSvc := metrics.NewService(metrics.ServiceOpts{
		Hostname:         opts.Hostname,
		Elector:          coord,
		KustoCli:         opts.MetricsKustoCli,
		PeerHealthReport: health,
	})

	dbs := make(map[string]struct{}, len(opts.AllowedDatabase))
	for _, db := range opts.AllowedDatabase {
		dbs[db] = struct{}{}
	}

	handler := metricsHandler.NewHandler(metricsHandler.HandlerOpts{
		RequestTransformer: &transform.RequestTransformer{
			DropLabels:      opts.DropLabels,
			DropMetrics:     opts.DropMetrics,
			AllowedDatabase: dbs,
		},
		RequestWriter: coord,
		HealthChecker: health,
	})

	_, l := logsv1connect.NewLogsServiceHandler(otlp.NewLogsServiceHandler(coord.WriteOTLPLogs, opts.LogsDatabases))

	databases := make(map[string]struct{})
	for _, db := range opts.LogsDatabases {
		databases[db] = struct{}{}
	}
	for _, db := range opts.MetricsDatabases {
		databases[db] = struct{}{}
	}

	return &Service{
		opts:        opts,
		databases:   databases,
		uploader:    opts.Uploader,
		replicator:  repl,
		store:       store,
		coordinator: coord,
		batcher:     batcher,
		metrics:     metricsSvc,
		handler:     handler,
		health:      health,
		logsHandler: l,
		requestFilter: &transform.RequestTransformer{
			DropMetrics: opts.DropMetrics,
			DropLabels:  opts.DropLabels,
		},
	}, nil
}

func (s *Service) Open(ctx context.Context) error {
	var svcCtx context.Context
	svcCtx, s.closeFn = context.WithCancel(ctx)
	if err := s.uploader.Open(svcCtx); err != nil {
		return err
	}

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

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-svcCtx.Done():
				return
			case <-ticker.C:
				metrics.IngestorHealthCheck.WithLabelValues(s.opts.Region).Set(1)
			}
		}
	}()

	return nil
}

func (s *Service) Close() error {
	s.closeFn()

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

	if err := s.uploader.Close(); err != nil {
		return err
	}

	return s.store.Close()
}

// HandleReceive handles the prometheus remote write requests and writes them to the store.
func (s *Service) HandleReceive(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleReceive(w, r)
}

// HandleLogs handles OTLP logs requests and writes them to the store.
func (s *Service) HandleLogs(w http.ResponseWriter, r *http.Request) {
	s.logsHandler.ServeHTTP(w, r)
}

// HandleTransfer handles the transfer WAL segments from other nodes in the cluster.
func (s *Service) HandleTransfer(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	m := metrics.RequestsReceived.MustCurryWith(prometheus.Labels{"path": "/transfer"})
	filename := r.URL.Query().Get("filename")
	if filename == "" {
		m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
		http.Error(w, "missing filename", http.StatusBadRequest)
		return
	}

	defer func() {
		dur := time.Since(start)
		if dur.Seconds() > 10 {
			logger.Warnf("slow request: path=/transfer duration=%s from=%s size=%d file=%s", dur.String(), r.RemoteAddr, r.ContentLength, filename)
		}
		if err := r.Body.Close(); err != nil {
			logger.Errorf("close http body: %s, path=/transfer duration=%s", err.Error(), dur.String())
		}
	}()

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
		http.Error(w, "filename is invalid", http.StatusBadRequest)
		return
	}

	n, err := s.store.Import(f, r.Body)
	if errors.Is(err, wal.ErrMaxSegmentsExceeded) || errors.Is(err, wal.ErrMaxDiskUsageExceeded) {
		m.WithLabelValues(strconv.Itoa(http.StatusTooManyRequests)).Inc()
		http.Error(w, "Overloaded. Retry later", http.StatusTooManyRequests)
		return
	} else if err != nil {
		logger.Errorf("Failed to import %s: %s", filename, err.Error())
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

func (s *Service) UploadSegments() error {
	if err := s.batcher.BatchSegments(); err != nil {
		return err
	}
	logger.Infof("Waiting for upload queue to drain, %d batches remaining", len(s.uploader.UploadQueue()))
	logger.Infof("Waiting for transfer queue to drain, %d batches remaining", len(s.replicator.TransferQueue()))

	t := time.NewTicker(time.Second)
	defer t.Stop()
	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()

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
		case <-timeout.C:
			return fmt.Errorf("failed to upload segments")
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

	ext := filepath.Ext(filename)
	if ext != ".wal" {
		return ""
	}

	base := strings.Replace(filename, ext, "", 1)
	parts := strings.Split(base, "_")
	if len(parts) != 3 {
		return ""
	}

	db := parts[0]
	table := parts[1]
	epoch := parts[2]

	if db == "" || table == "" || epoch == "" {
		return ""
	}

	if invalidEntityCharacters.MatchString(db) || invalidEntityCharacters.MatchString(table) || invalidEntityCharacters.MatchString(epoch) {
		return ""
	}

	if _, ok := s.databases[db]; !ok {
		return ""
	}

	return fmt.Sprintf("%s_%s_%s.wal", db, table, epoch)
}
