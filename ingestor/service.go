package ingestor

import (
	"bytes"
	"context"
	"github.com/Azure/adx-mon/adx"
	"github.com/Azure/adx-mon/cluster"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pool"
	"github.com/Azure/adx-mon/prompb"
	"github.com/Azure/adx-mon/storage"
	"github.com/golang/snappy"
	"io"
	"k8s.io/client-go/kubernetes"
	"net/http"
	"time"
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
	walOpts storage.WALOpts
	opts    ServiceOpts

	compressor  storage.Compressor
	ingestor    adx.Uploader
	replicator  cluster.Replicator
	coordinator cluster.Coordinator
	archiver    cluster.Archiver
	closeFn     context.CancelFunc

	store   storage.Store
	metrics metrics.Service
}

type ServiceOpts struct {
	StorageDir     string
	Uploader       adx.Uploader
	MaxSegmentSize int64
	MaxSegmentAge  time.Duration

	UseCLIAuth bool

	// Dimensions is a slice of column=value elements where each element will be added all rows.
	Dimensions []string
	K8sCli     *kubernetes.Clientset
}

func NewService(opts ServiceOpts) (*Service, error) {
	walOpts := storage.WALOpts{
		StorageDir:     opts.StorageDir,
		SegmentMaxSize: opts.MaxSegmentSize,
		SegmentMaxAge:  opts.MaxSegmentAge,
	}

	c := storage.NewCompressor()

	store := storage.NewLocalStore(storage.StoreOpts{
		StorageDir:     opts.StorageDir,
		SegmentMaxSize: opts.MaxSegmentSize,
		SegmentMaxAge:  opts.MaxSegmentAge,
		Compressor:     c,
	})

	coord, err := cluster.NewCoordinator(&cluster.CoordinatorOpts{
		WriteTimeSeriesFn: store.WriteTimeSeries,
		K8sCli:            opts.K8sCli,
	})
	if err != nil {
		return nil, err
	}

	repl, err := cluster.NewReplicator(cluster.ReplicatorOpts{Partitioner: coord})
	if err != nil {
		return nil, err
	}

	archiver := cluster.NewArchiver(cluster.ArchiverOpts{
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
		compressor:  c,
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

	if err := s.compressor.Open(svcCtx); err != nil {
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

	if err := s.compressor.Close(); err != nil {
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

	//bodyBuf := bytes.NewBuffer(make([]byte, 0, 1024*102))
	_, err := io.Copy(bodyBuf, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	compressed := bodyBuf.Bytes()
	buf := bytesPool.Get(1024 * 1024)
	defer bytesPool.Put(buf)
	buf = buf[:0]

	//buf := make([]byte, 0, 1024*1024)
	reqBuf, err := snappy.Decode(buf, compressed)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	req := writeReqPool.Get(2500).(prompb.WriteRequest)
	defer writeReqPool.Put(req)
	req.Reset()

	//req := &prompb.WriteRequest{}
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
