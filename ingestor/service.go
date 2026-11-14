package ingestor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/ingestor/adx"
	"github.com/Azure/adx-mon/ingestor/cluster"
	ingestormetrics "github.com/Azure/adx-mon/ingestor/metrics"
	ingestorstorage "github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/debug"
	adxhttp "github.com/Azure/adx-mon/pkg/http"
	"github.com/Azure/adx-mon/pkg/ingestpolicy"
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

var transferGzipReaders = sync.Pool{New: func() any { return new(gzip.Reader) }}

// A valid empty gzip stream lets Reset release references to the previous
// request while retaining the decoder's buffers for reuse.
var emptyTransferGzip = []byte{0x1f, 0x8b, 8, 0, 0, 0, 0, 0, 0, 0xff, 1, 0, 0, 0xff, 0xff, 0, 0, 0, 0, 0, 0}

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

	uploader    Uploader
	replicator  cluster.Replicator
	coordinator cluster.Coordinator
	batcher     cluster.Batcher
	closeFn     context.CancelFunc

	store   storage.Store
	metrics ingestormetrics.Service

	scheduler *scheduler.Periodic
	// localScheduler runs tasks on every ingestor rather than only the leader, for work on local state such as the
	// WAL segments on this host.
	localScheduler *scheduler.Periodic

	dropFilePrefixes []string
	health           interface{ IsHealthy() bool }
}

const (
	// DefaultUnownedSegmentMaxAge is the default max age of segments of tables owned by another ingestor.
	DefaultUnownedSegmentMaxAge = time.Second

	// DefaultTransferLinger is the default time a closed segment of a table owned by another ingestor waits to be
	// batched with others before it is transferred.  Transfers waiting for the owner are merged, so it can be shorter
	// than the upload linger.
	DefaultTransferLinger = time.Second
)

type ServiceOpts struct {
	StorageDir     string
	Uploader       Uploader
	MaxSegmentSize int64
	MaxSegmentAge  time.Duration

	// UnownedSegmentMaxAge, when positive, replaces MaxSegmentAge for queued tables owned by another ingestor while
	// peer transfers are enabled, so their data reaches the owner sooner.
	UnownedSegmentMaxAge time.Duration

	K8sCli     kubernetes.Interface
	K8sCtrlCli client.Client

	// MetricsKustoCli is the Kusto client connected to the metrics kusto cluster.
	MetricsKustoCli []ingestormetrics.StatementExecutor

	// LogsKustoCli is the Kusto client connected to the logs kusto cluster.
	LogsKustoCli []ingestormetrics.StatementExecutor

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
	MaxDiskUsage      int64
	MaxTableDiskUsage int64
	// MaxIntakeMemory bounds the bytes buffered across concurrent transfer
	// requests. Zero disables the ceiling.
	MaxIntakeMemory      int64
	MaxTableSegmentCount int64
	DisableTableLimits   bool
	EnforceTableLimits   bool
	TableLimits          []wal.TableLimit

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

	// MaxTransfersPerTable, when positive, is how many transfers of one table run at once.  Batches waiting behind a
	// running transfer are merged, so the owner receives fewer, larger transfers.  When zero, a table may use every
	// transfer worker but the reserved ones.
	MaxTransfersPerTable int

	// TransferLinger, when positive, is how long a closed segment of a table owned by another ingestor waits to be
	// batched with others before it is transferred.  When zero, it waits as long as owned segments.
	TransferLinger time.Duration

	// UploadLinger, when positive, is how long a closed segment of a table this ingestor owns waits to be batched
	// with others before it is uploaded.  When zero, it waits cluster.DefaultQueuedLinger.
	UploadLinger time.Duration

	// ConcurrentUploads is the maximum number of concurrent uploads.
	ConcurrentUploads int

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

	StorageBackend storage.Backend

	// Realtime configures realtime ingestion.  When nil or without realtime tables, all tables are queued.
	Realtime *RealtimeOpts

	// QueuedReservedWorkersPercent is the percentage of transfer workers reserved for queued batches.
	QueuedReservedWorkersPercent int

	// PeerListeners are called with the ingestor's peer count and rank when the service opens and when they change.
	PeerListeners []func(cluster.PeerInfo)
}

// RealtimeOpts configures realtime ingestion in the ingestor.
type RealtimeOpts struct {
	// Policy assigns ingestion priorities to tables.
	Policy *ingestpolicy.Policy

	// MaxSegmentAge is the max age of realtime WAL segments before they are rotated.
	MaxSegmentAge time.Duration

	// MaxBatchLatency is the max time a closed realtime segment waits to be batched.
	MaxBatchLatency time.Duration

	// MaxBatchBytes is the max size of a realtime batch in WAL bytes.
	MaxBatchBytes int64

	// ReservedDiskBytes is disk space reserved for realtime segments.
	ReservedDiskBytes int64
}

// enabled returns true if realtime tables are configured.
func (o *RealtimeOpts) enabled() bool {
	return o != nil && o.Policy.HasRealtime()
}

func NewService(opts ServiceOpts) (*Service, error) {
	if opts.StorageBackend == "" {
		opts.StorageBackend = storage.BackendADX
	}

	budgetOpts, err := wal.ResolveAdmissionWithEnforcement(wal.Usage{Bytes: opts.MaxDiskUsage, Segments: opts.MaxSegmentCount}, opts.MaxTableDiskUsage, opts.MaxTableSegmentCount, opts.DisableTableLimits, opts.EnforceTableLimits, opts.TableLimits)
	if err != nil {
		return nil, err
	}
	opts.MaxIntakeMemory = intakeCeiling(opts.MaxIntakeMemory, opts.MaxTransferSize)
	if opts.Realtime.enabled() {
		// Admission replaces the repository's disk checks, so it must also
		// hold back the space reserved for realtime tables.
		budgetOpts.Policy = opts.Realtime.Policy
		budgetOpts.RealtimeReservedBytes = opts.Realtime.ReservedDiskBytes
	}

	coord, err := cluster.NewCoordinator(&cluster.CoordinatorOpts{
		K8sCli:        opts.K8sCli,
		Hostname:      opts.Hostname,
		Namespace:     opts.Namespace,
		PartitionSize: opts.PartitionSize,
	})
	if err != nil {
		return nil, err
	}

	storeOpts := storage.StoreOpts{
		StorageDir:             opts.StorageDir,
		SegmentMaxSize:         opts.MaxSegmentSize,
		SegmentMaxAge:          opts.MaxSegmentAge,
		EnableWALFsync:         opts.EnableWALFsync,
		MaxDiskUsage:           opts.MaxDiskUsage,
		MaxIntakeMemory:        opts.MaxIntakeMemory,
		Admission:              wal.NewAdmission(budgetOpts),
		MetricComponent:        "ingestor",
		StartupOpenConcurrency: opts.ConcurrentUploads,
	}
	if !opts.DisablePeerTransfer && opts.UnownedSegmentMaxAge > 0 {
		storeOpts.UnownedSegmentMaxAge = opts.UnownedSegmentMaxAge
		storeOpts.Owned = ownedBy(coord, opts.Hostname)
	}
	var realtimeBatch cluster.RealtimeBatchOpts
	if opts.Realtime.enabled() {
		storeOpts.Policy = opts.Realtime.Policy
		storeOpts.Realtime = wal.RotationPolicy{SegmentMaxAge: opts.Realtime.MaxSegmentAge}
		storeOpts.RealtimeReservedDiskBytes = opts.Realtime.ReservedDiskBytes
		realtimeBatch = cluster.RealtimeBatchOpts{
			MaxBatchLatency: opts.Realtime.MaxBatchLatency,
			MaxBatchBytes:   opts.Realtime.MaxBatchBytes,
		}
	}
	store := storage.NewLocalStore(storeOpts)

	health := cluster.NewHealth(cluster.HealthOpts{
		UnhealthyTimeout: time.Minute,
		MaxSegmentCount:  opts.MaxSegmentCount,
		MaxDiskUsage:     opts.MaxDiskUsage,
	})

	repl, err := cluster.NewReplicator(cluster.ReplicatorOpts{
		Hostname:                     opts.Hostname,
		Partitioner:                  coord,
		InsecureSkipVerify:           opts.InsecureSkipVerify,
		Health:                       health,
		SegmentRemover:               store,
		MaxTransferConcurrency:       opts.MaxTransferConcurrency,
		MaxTransfersPerTable:         opts.MaxTransfersPerTable,
		MaxBatchSegments:             opts.MaxBatchSegments,
		MaxTransferSize:              opts.MaxTransferSize,
		DisableGzip:                  true,
		PeerTransfer:                 true,
		QueuedReservedWorkersPercent: opts.QueuedReservedWorkersPercent,
		TransferLatency:              metrics.IngestorTransferLatency,
		TransferOutcomes:             metrics.IngestorPeerTransfers,
		TransferSegments:             metrics.IngestorPeerTransferSegments,
	})
	if err != nil {
		return nil, err
	}

	initTransferMetrics()

	// Create ingestor-specific WAL segment metrics lazily to avoid exposing collector metrics
	ingestorSegmentsTotal, ingestorSegmentsSizeBytes, ingestorSegmentsMaxAge := metrics.NewIngestorSegmentMetrics()

	batcher, err := cluster.NewBatcher(cluster.BatcherOpts{
		StorageDir:              opts.StorageDir,
		MaxSegmentAge:           opts.MaxSegmentAge,
		MaxTransferSize:         opts.MaxTransferSize,
		MaxTransferAge:          opts.MaxTransferAge,
		MaxBatchSegments:        opts.MaxBatchSegments,
		MaxTableBatches:         max(10, opts.ConcurrentUploads*2, opts.MaxTransferConcurrency*2),
		TransferLinger:          opts.TransferLinger,
		QueuedLinger:            opts.UploadLinger,
		Partitioner:             coord,
		Segmenter:               store.Index(),
		Usage:                   store,
		UploadQueue:             opts.Uploader.UploadQueue(),
		RealtimeUploadQueue:     realtimeUploadQueue(opts.Uploader),
		TransferQueue:           repl.TransferQueue(),
		RealtimeTransferQueue:   repl.RealtimeTransferQueue(),
		Realtime:                realtimeBatch,
		PeerHealthReporter:      health,
		TransfersDisabled:       opts.DisablePeerTransfer,
		SegmentsCountMetric:     ingestorSegmentsTotal,
		SegmentsSizeBytesMetric: ingestorSegmentsSizeBytes,
		SegmentsMaxAgeMetric:    ingestorSegmentsMaxAge,

		SegmentsSizeByPriorityMetric: metrics.NewIngestorSegmentSizeByPriorityMetric(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create batcher: %w", err)
	}

	health.QueueSizer = &queueSizerAdapter{
		store:   store,
		batcher: batcher,
	}

	allKustoCli := make([]ingestormetrics.StatementExecutor, 0, len(opts.MetricsKustoCli)+len(opts.LogsKustoCli))
	allKustoCli = append(allKustoCli, opts.MetricsKustoCli...)
	allKustoCli = append(allKustoCli, opts.LogsKustoCli...)

	metricsSvc := ingestormetrics.NewService(ingestormetrics.ServiceOpts{
		Elector:          coord,
		Region:           opts.Region,
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
		localScheduler:   scheduler.NewScheduler(nil),
		dropFilePrefixes: opts.DropFilePrefixes,
	}, nil
}

// ownedBy returns a func that reports whether hostname owns the table of a WAL prefix.  A table is treated as owned
// while its owner is unknown, before the peers are known.
func ownedBy(p cluster.MetricPartitioner, hostname string) func(prefix string) bool {
	return func(prefix string) bool {
		owner, _ := p.Owner([]byte(prefix))
		return owner == "" || owner == hostname
	}
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

	for _, listener := range s.opts.PeerListeners {
		s.coordinator.SubscribePeers(listener)
		listener(s.coordinator.Peers())
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

	if err := s.localScheduler.Open(svcCtx); err != nil {
		return err
	}

	s.scheduler.ScheduleEvery(time.Minute, "ingestor-health-check", func(ctx context.Context) error {
		metrics.IngestorHealthCheck.WithLabelValues(s.opts.Region).Set(1)
		return nil
	})

	if s.opts.StorageBackend == storage.BackendADX {
		fnStore := ingestorstorage.NewFunctions(s.opts.K8sCtrlCli, s.coordinator)
		crdStore := ingestorstorage.NewCRDHandler(s.opts.K8sCtrlCli, s.coordinator)
		functionClients := make([]adx.StatementExecutor, 0, len(s.opts.MetricsKustoCli)+len(s.opts.LogsKustoCli))
		for _, client := range s.opts.MetricsKustoCli {
			functionClients = append(functionClients, client)
		}
		for _, client := range s.opts.LogsKustoCli {
			functionClients = append(functionClients, client)
		}
		functions := adx.NewSyncFunctionsTask(fnStore, functionClients, s.opts.ClusterLabels)
		s.scheduler.ScheduleEvery(time.Minute, "sync-functions", functions.Run)

		for _, v := range s.opts.MetricsKustoCli {
			t := adx.NewDropUnusedTablesTask(v)
			s.scheduler.ScheduleEvery(12*time.Hour, "delete-unused-tables", func(ctx context.Context) error {
				return t.Run(ctx)
			})

			m := adx.NewManagementCommandsTask(crdStore, v, s.opts.ClusterLabels)
			s.scheduler.ScheduleEvery(10*time.Minute, "management-commands", func(ctx context.Context) error {
				return m.Run(ctx)
			})

			sr := adx.NewSummaryRuleTask(crdStore, v, s.opts.ClusterLabels)
			s.scheduler.ScheduleEvery(time.Minute, "summary-rules", func(ctx context.Context) error {
				return sr.Run(ctx)
			})
		}

		for _, v := range s.opts.LogsKustoCli {
			m := adx.NewManagementCommandsTask(crdStore, v, s.opts.ClusterLabels)
			s.scheduler.ScheduleEvery(10*time.Minute, "management-commands", func(ctx context.Context) error {
				return m.Run(ctx)
			})

			sr := adx.NewSummaryRuleTask(crdStore, v, s.opts.ClusterLabels)
			s.scheduler.ScheduleEvery(time.Minute, "summary-rules", func(ctx context.Context) error {
				return sr.Run(ctx)
			})
		}

	}

	if accounting, ok := s.store.(adx.SegmentAccounting); ok {
		var batches adx.BatchTracker
		if bt, ok := s.batcher.(adx.BatchTracker); ok {
			batches = bt
		}
		// Closed segments are normally uploaded within a few max segment ages.  Older ones are reported so a
		// segment that is stuck can be identified.
		staleAge := max(3*s.opts.MaxSegmentAge, 15*time.Minute)
		t := adx.NewAuditDiskSpaceTask(accounting, batches, s.opts.StorageDir, staleAge)
		// WAL segments and batch claims are local to each ingestor, so the audit runs on every instance.
		s.localScheduler.ScheduleEvery(5*time.Minute, "audit-disk-space", func(ctx context.Context) error {
			return t.Run(ctx)
		})
	}

	return nil
}

func (s *Service) Close() error {
	s.closeFn()

	if err := s.scheduler.Close(); err != nil {
		return err
	}

	if err := s.localScheduler.Close(); err != nil {
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

// transferStallTimeout is how long a transfer body may go without receiving any data before the request is abandoned.
var transferStallTimeout = 10 * time.Second

// stallReader extends the connection's read deadline before each read, so a sender that stops sending is cut off
// while a large body that keeps arriving is not.
type stallReader struct {
	io.ReadCloser
	rc *http.ResponseController
}

func (r stallReader) Read(p []byte) (int, error) {
	// Deadlines are unsupported by test recorders, where there is no connection to stall.
	_ = r.rc.SetReadDeadline(time.Now().Add(transferStallTimeout))
	return r.ReadCloser.Read(p)
}

// HandleTransfer handles the transfer WAL segments from other nodes in the cluster.
func (s *Service) HandleTransfer(w http.ResponseWriter, r *http.Request) {
	adxhttp.MaybeCloseConnection(w, r)

	start := time.Now()
	m := metrics.RequestsReceived.MustCurryWith(prometheus.Labels{"path": "/transfer"})
	sender := "collector"
	if r.Header.Get(cluster.PeerTransferHeader) != "" {
		sender = "peer"
	}
	status := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
	w = status
	defer func() { metrics.IngestorTransfersReceived.WithLabelValues(sender, strconv.Itoa(status.code)).Inc() }()
	filename := r.URL.Query().Get("filename")
	if filename == "" {
		m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
		http.Error(w, "missing filename", http.StatusBadRequest)
		return
	}
	requestId := r.Header.Get("X-Request-ID")

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

	cr := reader.NewCounterReader(stallReader{ReadCloser: r.Body, rc: http.NewResponseController(w)})
	var body io.ReadCloser = cr
	defer func() {
		io.Copy(io.Discard, body)

		metrics.RequestsBytesReceived.Add(float64(cr.Count()))
		dur := time.Since(start)
		if s.opts.SlowRequestThreshold > 0 && dur.Seconds() > s.opts.SlowRequestThreshold {
			logger.Warnf("Slow request: requestId=%s path=/transfer duration=%s from=%s size=%d file=%s", requestId, dur.String(), originalIP, cr.Count(), filename)
		}
		if err := body.Close(); err != nil {
			logger.Errorf("Close http body: requestId=%s %s, path=/transfer duration=%s from=%s", requestId, err.Error(), dur.String(), originalIP)
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
		r.Close = true
		w.Header().Set("Connection", "close")

		w.Header().Set(cluster.OverloadScopeHeader, "instance")
		w.Header().Set("Retry-After", "5")
		m.WithLabelValues(strconv.Itoa(http.StatusTooManyRequests)).Inc()
		http.Error(w, "Overloaded. Retry later", http.StatusTooManyRequests)
		return
	}

	// https://pkg.go.dev/io/fs#ValidPath
	// Check for possible traversal attacks.
	f := s.validateFileName(filename)
	if f == "" {
		logger.Errorf("Failed to import: invalid filename %s from=%s requestId=%s", filename, originalIP, requestId)
		m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
		http.Error(w, "Filename is invalid", http.StatusBadRequest)
		return
	}

	// If the request is gzipped, create a gzip reader to decompress the body.
	if r.Header.Get("Content-Encoding") == "gzip" {
		gzipReader := transferGzipReaders.Get().(*gzip.Reader)
		err := gzipReader.Reset(body)
		if err != nil {
			// Reset can fail after partially initializing the decompressor. Do
			// not pool a reader whose state may be incomplete.
			logger.Errorf("Failed to import: create gzip reader: %s from=%s requestId=%s", err.Error(), originalIP, requestId)
			m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
			http.Error(w, "Invalid gzip encoding", http.StatusBadRequest)
			return
		}
		defer func() {
			_ = gzipReader.Close()
			// Reset clears references to the request body and header fields while
			// retaining the decompressor for the next transfer.
			if err := gzipReader.Reset(bytes.NewReader(emptyTransferGzip)); err != nil {
				return
			}
			transferGzipReaders.Put(gzipReader)
		}()
		body = gzipReader
	}

	n, err := s.store.Import(f, body)
	if oversized, ok := errors.AsType[*wal.SegmentTooLargeError](err); ok {
		// Permanent for this payload: tell the sender how much this destination
		// can accept so it can split rather than retry the same batch forever.
		w.Header().Set(cluster.MaxTransferBytesHeader, strconv.FormatInt(oversized.Limit, 10))
		m.WithLabelValues(strconv.Itoa(http.StatusRequestEntityTooLarge)).Inc()
		http.Error(w, oversized.Error(), http.StatusRequestEntityTooLarge)
		return
	}
	if quota, ok := errors.AsType[*wal.TableOverloadedError](err); ok {
		w.Header().Set(cluster.OverloadScopeHeader, "table")
		w.Header().Set(cluster.OverloadReasonHeader, quota.Reason)
		w.Header().Set("Retry-After", strconv.Itoa(int(quota.RetryAfter.Seconds())))
		m.WithLabelValues(strconv.Itoa(http.StatusTooManyRequests)).Inc()
		http.Error(w, quota.Error(), http.StatusTooManyRequests)
		return
	}
	if errors.Is(err, wal.ErrMaxSegmentsExceeded) || errors.Is(err, wal.ErrMaxDiskUsageExceeded) || errors.Is(err, storage.ErrIntakeMemoryExhausted) {
		w.Header().Set(cluster.OverloadScopeHeader, "instance")
		w.Header().Set("Retry-After", "5")
		r.Close = true
		w.Header().Set("Connection", "close")

		m.WithLabelValues(strconv.Itoa(http.StatusTooManyRequests)).Inc()
		http.Error(w, "Overloaded. Retry later", http.StatusTooManyRequests)
		return
	} else if errors.Is(err, wal.ErrSegmentLocked) {
		m.WithLabelValues(strconv.Itoa(http.StatusLocked)).Inc()
		http.Error(w, err.Error(), http.StatusLocked)
		return
	} else if err != nil && strings.Contains(err.Error(), "block checksum verification failed") {
		logger.Errorf("Failed to import checksum error: %s from=%s requestId=%s", filename, originalIP, requestId)
		m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
		http.Error(w, "Block checksum verification failed", http.StatusBadRequest)
		return
	} else if err != nil {
		r.Close = true
		w.Header().Set("Connection", "close")

		logger.Errorf("Failed to import %s: %s from=%s requestId=%s", filename, err.Error(), originalIP, requestId)
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
	if err := s.DisableWrites(); err != nil {
		return err
	}

	if err := s.UploadSegments(ctx); err != nil {
		return fmt.Errorf("Failed to upload segments: %s", err.Error())
	}

	return nil
}

// stallTimeout is how long the drain waits without any progress before giving
// up. A destination that cannot drain must not hold shutdown open for the whole
// context deadline.
var stallTimeout = 30 * time.Second

func (s *Service) UploadSegments(ctx context.Context) error {
	if err := s.batcher.BatchSegments(); err != nil {
		return err
	}
	logger.Infof("Waiting for upload queue to drain, %d batches remaining", s.uploadQueueLen())
	logger.Infof("Waiting for transfer queue to drain, %d batches remaining", s.transferQueueLen())

	t := time.NewTicker(time.Second)
	defer t.Stop()

	remaining := s.pendingSegments()
	lastProgress := time.Now()
	for {
		if remaining == 0 {
			return nil
		}
		select {
		case <-t.C:
			if n := s.uploadQueueLen(); n != 0 {
				logger.Infof("Waiting for upload queue to drain, %d batches remaining", n)
			}
			if n := s.transferQueueLen(); n != 0 {
				logger.Infof("Waiting for transfer queue to drain, %d batches remaining", n)
			}

			current := s.pendingSegments()
			if current == 0 {
				return nil
			}
			if current < remaining {
				remaining = current
				lastProgress = time.Now()
				continue
			}
			remaining = current
			if time.Since(lastProgress) >= stallTimeout {
				// A destination that cannot drain must not hold shutdown open
				// for the whole deadline. Remaining work stays on disk and
				// resumes after restart.
				logger.Warnf("Backlog stopped draining with %d segments remaining; continuing shutdown", remaining)
				return nil
			}
		case <-ctx.Done():
			return fmt.Errorf("timed out to upload segments")
		}
	}
}

// realtimeUploader is implemented by uploaders that accept realtime batches on a separate queue.
type realtimeUploader interface {
	RealtimeUploadQueue() chan *cluster.Batch
}

// realtimeUploadQueue returns the realtime upload queue of u, or nil if u does not have one.
func realtimeUploadQueue(u Uploader) chan *cluster.Batch {
	if ru, ok := u.(realtimeUploader); ok {
		return ru.RealtimeUploadQueue()
	}
	return nil
}

// uploadQueueLen returns the number of batches waiting to be uploaded.
func (s *Service) uploadQueueLen() int {
	return len(s.uploader.UploadQueue()) + len(realtimeUploadQueue(s.uploader))
}

// pendingSegments returns the number of closed segments not yet uploaded or transferred.  The upload and transfer
// schedulers take batches off their queues as soon as they are queued, so the queue lengths miss most pending work.
func (s *Service) pendingSegments() int {
	return int(s.batcher.SegmentsTotal())
}

// transferQueueLen returns the number of batches waiting to be transferred to peers.
func (s *Service) transferQueueLen() int {
	return len(s.replicator.TransferQueue()) + len(s.replicator.RealtimeTransferQueue())
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

// queueSizerAdapter is an adapter that implements the cluster.QueueSizer interface by wrapping the
// storage.LocalStore and cluster.Batcher interfaces. It provides methods to get the sizes of the transfer
// and upload queues, the total number of segments, the size of segments, and the maximum age of segments.
type queueSizerAdapter struct {
	store   *storage.LocalStore
	batcher cluster.Batcher
}

// QueueUsage reads admission accounting once for both health thresholds.
func (q *queueSizerAdapter) QueueUsage() (segments, bytes int64) {
	if q.store != nil {
		u, ok := q.store.AdmissionUsage()
		if ok {
			return u.Segments, u.Bytes
		}
	}
	return q.SegmentsTotal(), q.SegmentsSize()
}

func (q *queueSizerAdapter) TransferQueueSize() int {
	return q.batcher.TransferQueueSize()
}

func (q *queueSizerAdapter) UploadQueueSize() int {
	return q.batcher.UploadQueueSize()
}

func (q *queueSizerAdapter) SegmentsTotal() int64 {
	return q.store.SegmentCount()
}

func (q *queueSizerAdapter) SegmentsSize() int64 {
	return q.store.Size()
}

func (q *queueSizerAdapter) MaxSegmentAge() time.Duration {
	return q.store.Index().OldestSegmentAge()
}

// statusRecorder records the status code written to a response.
type statusRecorder struct {
	http.ResponseWriter
	code    int
	written bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.written {
		s.code, s.written = code, true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.written = true
	return s.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (s *statusRecorder) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}

// initTransferMetrics creates the transfer series an ingestor reports, at zero, so the first transfer of each kind is
// counted as an increase rather than appearing as a new series that rate() and increase() miss.  Transfer status codes
// beyond the common ones are created as they occur.
func initTransferMetrics() {
	for _, outcome := range cluster.TransferOutcomeNames {
		metrics.IngestorPeerTransfers.WithLabelValues(outcome)
	}
	for _, sender := range []string{"collector", "peer"} {
		for _, code := range []string{"202", "423", "429"} {
			metrics.IngestorTransfersReceived.WithLabelValues(sender, code)
		}
	}
	for _, code := range []string{"202", "423", "429"} {
		metrics.RequestsReceived.WithLabelValues("/transfer", code)
	}
}

// intakeCeiling returns the intake memory ceiling to use for a configured ceiling and the largest transfer.  Transfers
// are buffered in full before admission, so a ceiling below what the largest transfer is charged would reject that
// transfer on every retry; such a ceiling is raised to what it is charged.  Zero disables the ceiling.
func intakeCeiling(configured, maxTransferSize int64) int64 {
	if configured <= 0 {
		return configured
	}
	need := storage.IntakeNeeded(maxTransferSize)
	if configured >= need {
		return configured
	}
	logger.Warnf("Raising max intake memory from %d to %d, the most a transfer of max transfer size (%d) is charged", configured, need, maxTransferSize)
	return need
}
