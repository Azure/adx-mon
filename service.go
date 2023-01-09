package adx_mon

import (
	"bytes"
	"github.com/Azure/adx-mon/adx"
	"github.com/Azure/adx-mon/cluster"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/adx-mon/pool"
	"github.com/Azure/adx-mon/prompb"
	"github.com/Azure/adx-mon/storage"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/golang/snappy"
	"io"
	"net/http"
	"time"
)

var (
	bytesBufPool = pool.NewGeneric(10, func(sz int) interface{} {
		return bytes.NewBuffer(make([]byte, 0, sz))
	})

	bytesPool = pool.NewBytes(10)

	writeReqPool = pool.NewGeneric(10, func(sz int) interface{} {
		return prompb.WriteRequest{
			Timeseries: make([]prompb.TimeSeries, 0, sz),
		}
	})
)

type Service struct {
	walOpts storage.WALOpts
	opts    ServiceOpts

	compressor  *storage.Compressor
	ingestor    *adx.Ingestor
	coordinator *cluster.Coordinator
	closing     chan struct{}

	store *storage.Store
}

type ServiceOpts struct {
	StorageDir        string
	KustoEndpoint     string
	Database          string
	ConcurrentUploads int
	MaxSegmentSize    int64
	MaxSegmentAge     time.Duration

	UseCLIAuth bool

	// Dimensions is a slice of column=value elements where each element will be added all rows.
	Dimensions []string
}

func NewService(opts ServiceOpts) (*Service, error) {

	var (
		authConfig autorest.Authorizer
		err        error
	)

	if opts.UseCLIAuth {
		authConfig, err = auth.NewAuthorizerFromCLIWithResource(opts.KustoEndpoint)
		if err != nil {
			return nil, err
		}
	} else {
		authConfig, err = auth.NewAuthorizerFromEnvironmentWithResource(opts.KustoEndpoint)
		if err != nil {
			return nil, err
		}
	}

	client, err := kusto.New(opts.KustoEndpoint, kusto.Authorization{Authorizer: authConfig})
	if err != nil {
		return nil, err
	}
	defer client.Close()

	ing := adx.NewIngestor(client, adx.IngesterOpts{
		StorageDir:        opts.StorageDir,
		Database:          opts.Database,
		ConcurrentUploads: opts.ConcurrentUploads,
		Dimensions:        opts.Dimensions,
	})

	walOpts := storage.WALOpts{
		StorageDir:     opts.StorageDir,
		SegmentMaxSize: opts.MaxSegmentSize,
		SegmentMaxAge:  opts.MaxSegmentAge,
	}

	c := &storage.Compressor{}

	store := storage.NewStore(storage.StoreOpts{
		StorageDir:     opts.StorageDir,
		SegmentMaxSize: opts.MaxSegmentSize,
		SegmentMaxAge:  opts.MaxSegmentAge,
	})

	coord, err := cluster.NewCoordinator(&cluster.CoordinatorOpts{
		WriteTimeSeriesFn: store.WriteTimeSeries,
	})
	if err != nil {
		return nil, err
	}

	return &Service{
		opts:        opts,
		walOpts:     walOpts,
		ingestor:    ing,
		store:       store,
		coordinator: coord,
		compressor:  c,
		closing:     make(chan struct{}),
	}, nil
}

func (s *Service) Open() error {
	if err := s.ingestor.Open(); err != nil {
		return err
	}

	if err := s.compressor.Open(); err != nil {
		return err
	}
	if err := s.store.Open(); err != nil {
		return err
	}

	if err := s.coordinator.Open(); err != nil {
		return err
	}

	//go s.rotate()
	return nil
}

func (s *Service) Close() error {
	close(s.closing)

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

// handleReceive extracts prometheus samples from incoming request and sends them to statsd
func (s *Service) HandleReceive(w http.ResponseWriter, r *http.Request) {

	bodyBuf := bytesBufPool.Get(1024 * 1024).(*bytes.Buffer)
	bodyBuf.Reset()
	defer bytesBufPool.Put(bodyBuf)

	_, err := io.Copy(bodyBuf, r.Body)
	defer func() {
		if err := r.Body.Close(); err != nil {
			logger.Error("close http body: %s", err.Error())
		}
	}()
	compressed := bodyBuf.Bytes()

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	buf := bytesPool.Get(1024 * 1024)
	buf = buf[:0]
	reqBuf, err := snappy.Decode(buf, compressed)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	req := writeReqPool.Get(2500).(prompb.WriteRequest)
	req.Reset()
	if err := req.Unmarshal(reqBuf); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	bytesPool.Put(buf)
	defer writeReqPool.Put(req)

	for _, ts := range req.Timeseries {

		// Drop histogram and
		for _, v := range ts.Labels {
			if bytes.Equal(v.Name, []byte("le")) {
				continue
			}
		}

		if err := s.coordinator.Write(ts); err != nil {
			logger.Error("Failed to write ts: %s", err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

	w.WriteHeader(http.StatusAccepted)
}
