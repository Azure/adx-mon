package otlp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/logs/v1/logsv1connect"
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	"github.com/Azure/adx-mon/ingestor/adx"
	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/otlp"
	connect_go "github.com/bufbuild/connect-go"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"
	"k8s.io/client-go/kubernetes"
)

type LogsService struct {
	Uploaders    map[string]adx.Uploader
	Stores       map[string]*storage.LocalStore
	Coordinators map[string]cluster.Coordinator
	Replicators  map[string]cluster.Replicator
	Batchers     map[string]cluster.Batcher
	closeFn      context.CancelFunc
	transferPath string
}

type LogsServiceOpts struct {
	StorageDir string
	Uploaders  []adx.Uploader

	MaxSegmentSize int64
	MaxSegmentAge  time.Duration
	K8sCli         kubernetes.Interface

	// InsecureSkipVerify disables TLS certificate verification.
	InsecureSkipVerify bool

	// Namespace is the namespace used for peer discovery.
	Namespace string

	// Hostname is the hostname of the current node.
	Hostname string
}

func NewLogsService(opts LogsServiceOpts) (*LogsService, error) {
	s := &LogsService{
		Uploaders:    make(map[string]adx.Uploader),
		Stores:       make(map[string]*storage.LocalStore),
		Coordinators: make(map[string]cluster.Coordinator),
		Replicators:  make(map[string]cluster.Replicator),
		Batchers:     make(map[string]cluster.Batcher),
		transferPath: "/transfer/logs",
	}
	for _, uploader := range opts.Uploaders {
		db := uploader.Database()
		dir := filepath.Join(opts.StorageDir, db)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create storage directory %s: %w", dir, err)
		}

		s.Uploaders[db] = uploader

		s.Stores[db] = storage.NewLocalStore(storage.StoreOpts{
			StorageDir:     dir,
			SegmentMaxSize: opts.MaxSegmentSize,
			SegmentMaxAge:  opts.MaxSegmentAge,
		})

		var err error
		s.Coordinators[db], err = cluster.NewCoordinator(&cluster.CoordinatorOpts{
			K8sCli:    opts.K8sCli,
			Hostname:  opts.Hostname,
			Namespace: opts.Namespace,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create cluster coordinator for database=%s: %w", db, err)
		}

		s.Replicators[db], err = cluster.NewReplicator(cluster.ReplicatorOpts{
			Hostname:           opts.Hostname,
			Partitioner:        s.Coordinators[db],
			InsecureSkipVerify: opts.InsecureSkipVerify,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create cluster replicator for database=%s: %w", db, err)
		}

		s.Batchers[db] = cluster.NewBatcher(cluster.BatcherOpts{
			StorageDir:    dir,
			Partitioner:   s.Coordinators[db],
			Segmenter:     s.Stores[db],
			UploadQueue:   uploader.UploadQueue(),
			TransferQueue: s.Replicators[db].TransferQueue(),
		})

	}
	return s, nil
}

func (svc *LogsService) Open(ctx context.Context) error {
	var svcCtx context.Context
	svcCtx, svc.closeFn = context.WithCancel(ctx)
	for db, uploader := range svc.Uploaders {
		if err := uploader.Open(svcCtx); err != nil {
			return fmt.Errorf("failed to open uploader for database=%s: %w", db, err)
		}
	}
	for db, store := range svc.Stores {
		if err := store.Open(svcCtx); err != nil {
			return fmt.Errorf("failed to open store for database=%s: %w", db, err)
		}
	}
	for db, coordinator := range svc.Coordinators {
		if err := coordinator.Open(svcCtx); err != nil {
			return fmt.Errorf("failed to open coordinator for database=%s: %w", db, err)
		}
	}
	for db, batcher := range svc.Batchers {
		if err := batcher.Open(svcCtx); err != nil {
			return fmt.Errorf("failed to open batcher for database=%s: %w", db, err)
		}
	}
	for db, replicator := range svc.Replicators {
		if err := replicator.Open(svcCtx); err != nil {
			return fmt.Errorf("failed to open replicator for database=%s: %w", db, err)
		}
	}
	return nil
}

func (svc *LogsService) Close() error {
	svc.closeFn()
	for db, replicator := range svc.Replicators {
		if err := replicator.Close(); err != nil {
			return fmt.Errorf("failed to close replicator for database=%s: %w", db, err)
		}
	}
	for db, batcher := range svc.Batchers {
		if err := batcher.Close(); err != nil {
			return fmt.Errorf("failed to close batcher for database=%s: %w", db, err)
		}
	}
	for db, coordinator := range svc.Coordinators {
		if err := coordinator.Close(); err != nil {
			return fmt.Errorf("failed to close coordinator for database=%s: %w", db, err)
		}
	}
	for db, uploader := range svc.Uploaders {
		if err := uploader.Close(); err != nil {
			return fmt.Errorf("failed to close uploader for database=%s: %w", db, err)
		}
	}
	for db, store := range svc.Stores {
		if err := store.Close(); err != nil {
			return fmt.Errorf("failed to close store for database=%s: %w", db, err)
		}
	}
	return nil
}

func (svc *LogsService) HandleReceive() (string, http.Handler) {
	return logsv1connect.NewLogsServiceHandler(svc)
}

func (svc *LogsService) Export(ctx context.Context, req *connect_go.Request[v1.ExportLogsServiceRequest]) (*connect_go.Response[v1.ExportLogsServiceResponse], error) {
	var (
		m       = metrics.RequestsReceived.MustCurryWith(prometheus.Labels{"path": logsv1connect.LogsServiceExportProcedure})
		grouped = otlp.GroupByKustoTable(req.Msg)

		mu       sync.Mutex
		rejected int
	)

	if logger.IsDebug() {
		for _, g := range grouped {
			logger.Debug("Grouped %d records for database=%s, table=%s", g.NumberOfRecords, g.Database, g.Table)
		}
	}

	g, gctx := errgroup.WithContext(ctx)
	for _, group := range grouped {
		group := group
		g.Go(func() error {
			metrics.LogsReceived.WithLabelValues(group.Database, group.Table).Add(float64(group.NumberOfRecords))
			store, ok := svc.Stores[group.Database]
			if !ok {
				return fmt.Errorf("no store found for database=%s", group.Database)
			}
			if err := store.WriteOTLPLogs(gctx, group); err != nil {
				mu.Lock()
				rejected += group.NumberOfRecords
				mu.Unlock()
				return fmt.Errorf("failed to write logs to store: %w", err)
			}
			metrics.LogsStored.WithLabelValues(group.Database, group.Table).Add(float64(group.NumberOfRecords))
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
		mu.Lock()
		res := &v1.ExportLogsServiceResponse{
			PartialSuccess: &v1.ExportLogsPartialSuccess{
				RejectedLogRecords: int64(rejected),
				ErrorMessage:       err.Error(),
			},
		}
		mu.Unlock()
		return connect_go.NewResponse(res), err
	}

	m.WithLabelValues(strconv.Itoa(http.StatusOK)).Inc()
	return connect_go.NewResponse(&v1.ExportLogsServiceResponse{}), nil
}

func (svc *LogsService) HandleTransfer() (string, http.Handler) {

	return svc.transferPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := metrics.RequestsReceived.MustCurryWith(prometheus.Labels{"path": svc.transferPath})
		defer func() {
			if err := r.Body.Close(); err != nil {
				logger.Error("close http body: %s", err.Error())
			}
		}()

		database := r.URL.Query().Get("database")
		if database == "" {
			m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
			http.Error(w, "missing database", http.StatusBadRequest)
			return
		}

		filename := r.URL.Query().Get("filename")
		if filename == "" {
			m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
			http.Error(w, "missing filename", http.StatusBadRequest)
			return
		}

		store, ok := svc.Stores[database]
		if !ok {
			m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
			http.Error(w, fmt.Sprintf("no store found for database=%s", database), http.StatusBadRequest)
			return
		}
		if n, err := store.Import(filename, r.Body); err != nil {
			m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		} else {
			logger.Info("Imported %d bytes to %s", n, filename)
		}

		m.WithLabelValues(strconv.Itoa(http.StatusAccepted)).Inc()
		w.WriteHeader(http.StatusAccepted)
	})
}

func (svc *LogsService) UploadSegments() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	g, gctx := errgroup.WithContext(ctx)
	for db, batcher := range svc.Batchers {
		db := db
		batcher := batcher

		g.Go(func() error {
			if err := batcher.BatchSegments(); err != nil {
				return fmt.Errorf("failed to batch segments for database=%s: %w", db, err)
			}
			t := time.NewTicker(time.Second)
			for {
				select {
				case <-t.C:
					if len(svc.Uploaders[db].UploadQueue()) == 0 && len(svc.Replicators[db].TransferQueue()) == 0 {
						return nil
					}

					if len(svc.Uploaders[db].UploadQueue()) != 0 {
						logger.Info("Waiting for upload queue to drain %s, %d batches remaining", db, len(svc.Uploaders[db].UploadQueue()))
					}
					if len(svc.Replicators[db].TransferQueue()) != 0 {
						logger.Info("Waiting for transfer queue to drain %s, %d batches remaining", db, len(svc.Replicators[db].TransferQueue()))
					}
				case <-gctx.Done():
					return fmt.Errorf("failed to batch segments for database %s: %w", db, gctx.Err())
				}
			}
		})
	}
	return g.Wait()
}

func (svc *LogsService) DisableWrites() error {
	for db, store := range svc.Stores {
		if err := store.Close(); err != nil {
			return fmt.Errorf("failed to close store for database=%s: %w", db, err)
		}
	}
	return nil
}
