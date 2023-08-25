package ingestor

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/Azure/adx-mon/ingestor/adx"
	"github.com/Azure/adx-mon/ingestor/cluster"
	metricsHandler "github.com/Azure/adx-mon/ingestor/metrics"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/ingestor/transform"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/azure-kusto-go/kusto/ingest"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/kubernetes"
)

type Service struct {
	walOpts wal.WALOpts
	opts    ServiceOpts

	uploader    adx.Uploader
	replicator  cluster.Replicator
	coordinator cluster.Coordinator
	batcher     cluster.Batcher
	closeFn     context.CancelFunc

	store   storage.Store
	metrics metrics.Service

	handler       *metricsHandler.Handler
	requestFilter *transform.RequestFilter
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
	MetricsKustoCli ingest.QueryClient

	// InsecureSkipVerify disables TLS certificate verification.
	InsecureSkipVerify bool

	// Namespace is the namespace used for peer discovery.
	Namespace string

	// Hostname is the hostname of the current node.
	Hostname string

	// DisablePeerDiscovery disables peer discovery and prevents transfers of small segments to an owner.
	// Each instance of ingestor will upload received segments directly.
	DisablePeerDiscovery bool

	// MaxTransferSize is the minimum size of a segment that will be transferred to another node.  If a segment
	// exceeds this size, it will be uploaded directly by the current node.
	MaxTransferSize int64

	// MaxTransferAge is the maximum age of a segment that will be transferred to another node.  If a segment
	// exceeds this age, it will be uploaded directly by the current node.
	MaxTransferAge time.Duration

	// MetricsDatabase is the name of the metrics database.
	MetricsDatabase string
}

func NewService(opts ServiceOpts) (*Service, error) {
	store := storage.NewLocalStore(storage.StoreOpts{
		StorageDir:     opts.StorageDir,
		SegmentMaxSize: opts.MaxSegmentSize,
		SegmentMaxAge:  opts.MaxSegmentAge,
		LiftedColumns:  opts.LiftedColumns,
	})

	coord, err := cluster.NewCoordinator(&cluster.CoordinatorOpts{
		DisablePeerDiscovery: opts.DisablePeerDiscovery,
		WriteTimeSeriesFn:    store.WriteTimeSeries,
		K8sCli:               opts.K8sCli,
		Hostname:             opts.Hostname,
		Namespace:            opts.Namespace,
	})
	if err != nil {
		return nil, err
	}

	repl, err := cluster.NewReplicator(cluster.ReplicatorOpts{
		Hostname:           opts.Hostname,
		Partitioner:        coord,
		InsecureSkipVerify: opts.InsecureSkipVerify,
	})
	if err != nil {
		return nil, err
	}

	batcher := cluster.NewBatcher(cluster.BatcherOpts{
		StorageDir:      opts.StorageDir,
		MaxSegmentAge:   opts.MaxSegmentAge,
		MaxTransferSize: opts.MaxTransferSize,
		MaxTransferAge:  opts.MaxTransferAge,
		Partitioner:     coord,
		Segmenter:       store,
		UploadQueue:     opts.Uploader.UploadQueue(),
		TransferQueue:   repl.TransferQueue(),
	})

	metricsSvc := metrics.NewService(metrics.ServiceOpts{
		Hostname:    opts.Hostname,
		Partitioner: coord,
		KustoCli:    opts.MetricsKustoCli,
		Database:    opts.MetricsDatabase,
	})

	handler := metricsHandler.NewHandler(metricsHandler.HandlerOpts{
		DropLabels:    opts.DropLabels,
		DropMetrics:   opts.DropMetrics,
		SeriesCounter: metricsSvc,
		RequestWriter: coord,
		Database:      opts.Uploader.Database(),
	})

	return &Service{
		opts:        opts,
		uploader:    opts.Uploader,
		replicator:  repl,
		store:       store,
		coordinator: coord,
		batcher:     batcher,
		metrics:     metricsSvc,
		handler:     handler,
		requestFilter: &transform.RequestFilter{
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

// HandleTransfer handles the transfer WAL segments from other nodes in the cluster.
func (s *Service) HandleTransfer(w http.ResponseWriter, r *http.Request) {
	m := metrics.RequestsReceived.MustCurryWith(prometheus.Labels{"path": "/transfer"})
	defer func() {
		if err := r.Body.Close(); err != nil {
			logger.Error("close http body: %s", err.Error())
		}
	}()

	filename := r.URL.Query().Get("filename")
	if filename == "" {
		m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
		http.Error(w, "missing filename", http.StatusBadRequest)
		return
	}

	if n, err := s.store.Import(filename, r.Body); err != nil {
		m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	} else {
		logger.Info("Imported %d bytes to %s", n, filename)
	}
	m.WithLabelValues(strconv.Itoa(http.StatusAccepted)).Inc()
	w.WriteHeader(http.StatusAccepted)
}

func (s *Service) UploadSegments() error {
	if err := s.batcher.BatchSegments(); err != nil {
		return err
	}
	logger.Info("Waiting for upload queue to drain, %d batches remaining", len(s.uploader.UploadQueue()))
	logger.Info("Waiting for transfer queue to drain, %d batches remaining", len(s.replicator.TransferQueue()))

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
				logger.Info("Waiting for upload queue to drain, %d batches remaining", len(s.uploader.UploadQueue()))
			}
			if len(s.replicator.TransferQueue()) != 0 {
				logger.Info("Waiting for transfer queue to drain, %d batches remaining", len(s.replicator.TransferQueue()))
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
