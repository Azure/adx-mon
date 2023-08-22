package ingestor

import (
	"bytes"
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
	"github.com/Azure/adx-mon/pkg/pool"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/kubernetes"
)

var (
	bytesBufPool = pool.NewGeneric(50, func(sz int) interface{} {
		return bytes.NewBuffer(make([]byte, 0, sz))
	})

	bytesPool = pool.NewBytes(50)

	writeReqPool = pool.NewGeneric(50, func(sz int) interface{} {
		return prompb.WriteRequest{
			Timeseries: make([]prompb.TimeSeries, 0, sz),
		}
	})
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

	// InsecureSkipVerify disables TLS certificate verification.
	InsecureSkipVerify bool

	// Namespace is the namespace used for peer discovery.
	Namespace string

	// Hostname is the hostname of the current node.
	Hostname string
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
		K8sCli:            opts.K8sCli,
		Hostname:          opts.Hostname,
		Namespace:         opts.Namespace,
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
		StorageDir:     opts.StorageDir,
		MaxSegmentAge:  opts.MaxSegmentAge,
		MaxSegmentSize: opts.MaxSegmentSize,
		Partitioner:    coord,
		Segmenter:      store,
		UploadQueue:    opts.Uploader.UploadQueue(),
		TransferQueue:  repl.TransferQueue(),
	})

	metricsSvc := metrics.NewService(metrics.ServiceOpts{})

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
