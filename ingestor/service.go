package ingestor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"github.com/Azure/adx-mon/ingestor/adx"
	cluster2 "github.com/Azure/adx-mon/ingestor/cluster"
	storage2 "github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	pool2 "github.com/Azure/adx-mon/pkg/pool"
	prompb2 "github.com/Azure/adx-mon/pkg/prompb"
	"github.com/golang/snappy"
	"k8s.io/client-go/kubernetes"
)

var (
	bytesBufPool = pool2.NewGeneric(50, func(sz int) interface{} {
		return bytes.NewBuffer(make([]byte, 0, sz))
	})

	bytesPool = pool2.NewBytes(50)

	writeReqPool = pool2.NewGeneric(50, func(sz int) interface{} {
		return prompb2.WriteRequest{
			Timeseries: make([]prompb2.TimeSeries, 0, sz),
		}
	})
)

type Service struct {
	walOpts storage2.WALOpts
	opts    ServiceOpts

	ingestor    adx.Uploader
	replicator  cluster2.Replicator
	coordinator cluster2.Coordinator
	archiver    cluster2.Archiver
	closeFn     context.CancelFunc

	store   storage2.Store
	metrics metrics.Service
}

type ServiceOpts struct {
	StorageDir     string
	Uploader       adx.Uploader
	MaxSegmentSize int64
	MaxSegmentAge  time.Duration

	// Dimensions is a slice of column=value elements where each element will be added all rows.
	Dimensions []string
	K8sCli     *kubernetes.Clientset

	// InsecureSkipVerify disables TLS certificate verification.
	InsecureSkipVerify bool

	// Namespace is the namespace used for peer discovery.
	Namespace string

	// Hostname is the hostname of the current node.
	Hostname string
}

func NewService(opts ServiceOpts) (*Service, error) {
	walOpts := storage2.WALOpts{
		StorageDir:     opts.StorageDir,
		SegmentMaxSize: opts.MaxSegmentSize,
		SegmentMaxAge:  opts.MaxSegmentAge,
	}

	store := storage2.NewLocalStore(storage2.StoreOpts{
		StorageDir:     opts.StorageDir,
		SegmentMaxSize: opts.MaxSegmentSize,
		SegmentMaxAge:  opts.MaxSegmentAge,
	})

	coord, err := cluster2.NewCoordinator(&cluster2.CoordinatorOpts{
		WriteTimeSeriesFn: store.WriteTimeSeries,
		K8sCli:            opts.K8sCli,
		Hostname:          opts.Hostname,
		Namespace:         opts.Namespace,
	})
	if err != nil {
		return nil, err
	}

	repl, err := cluster2.NewReplicator(cluster2.ReplicatorOpts{
		Hostname:           opts.Hostname,
		Partitioner:        coord,
		InsecureSkipVerify: opts.InsecureSkipVerify,
	})
	if err != nil {
		return nil, err
	}

	archiver := cluster2.NewArchiver(cluster2.ArchiverOpts{
		StorageDir:    opts.StorageDir,
		Partitioner:   coord,
		Segmenter:     store,
		UploadQueue:   opts.Uploader.UploadQueue(),
		TransferQueue: repl.TransferQueue(),
	})

	metricsSvc := metrics.NewService(metrics.ServiceOpts{Coordinator: coord})

	return &Service{
		opts:        opts,
		walOpts:     walOpts,
		ingestor:    opts.Uploader,
		replicator:  repl,
		store:       store,
		coordinator: coord,
		archiver:    archiver,
		metrics:     metricsSvc,
	}, nil
}

func (s *Service) Open(ctx context.Context) error {
	var svcCtx context.Context
	svcCtx, s.closeFn = context.WithCancel(ctx)
	if err := s.ingestor.Open(svcCtx); err != nil {
		return err
	}

	if err := s.store.Open(svcCtx); err != nil {
		return err
	}

	if err := s.coordinator.Open(svcCtx); err != nil {
		return err
	}

	if err := s.archiver.Open(svcCtx); err != nil {
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

	if err := s.archiver.Close(); err != nil {
		return err
	}

	if err := s.coordinator.Close(); err != nil {
		return err
	}

	if err := s.ingestor.Close(); err != nil {
		return err
	}

	return s.store.Close()
}

// HandleReceive handles the prometheus remote write requests and writes them to the store.
func (s *Service) HandleReceive(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if err := r.Body.Close(); err != nil {
			logger.Error("close http body: %s", err.Error())
		}
	}()

	bodyBuf := bytesBufPool.Get(1024 * 1024).(*bytes.Buffer)
	defer bytesBufPool.Put(bodyBuf)
	bodyBuf.Reset()

	// bodyBuf := bytes.NewBuffer(make([]byte, 0, 1024*102))
	_, err := io.Copy(bodyBuf, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	compressed := bodyBuf.Bytes()
	buf := bytesPool.Get(1024 * 1024)
	defer bytesPool.Put(buf)
	buf = buf[:0]

	// buf := make([]byte, 0, 1024*1024)
	reqBuf, err := snappy.Decode(buf, compressed)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	req := writeReqPool.Get(2500).(prompb2.WriteRequest)
	defer writeReqPool.Put(req)
	req.Reset()

	// req := &prompb.WriteRequest{}
	if err := req.Unmarshal(reqBuf); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.coordinator.Write(r.Context(), req); err != nil {
		logger.Error("Failed to write ts: %s", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// HandleTransfer handles the transfer WAL segments from other nodes in the cluster.
func (s *Service) HandleTransfer(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if err := r.Body.Close(); err != nil {
			logger.Error("close http body: %s", err.Error())
		}
	}()

	filename := r.URL.Query().Get("filename")
	if filename == "" {
		http.Error(w, "missing filename", http.StatusBadRequest)
		return
	}

	if n, err := s.store.Import(filename, r.Body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	} else {
		logger.Info("Imported %d bytes to %s", n, filename)
	}
	w.WriteHeader(http.StatusAccepted)
}
