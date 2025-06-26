package collector

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http/pprof"
	"regexp"
	"time"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/metrics/v1/metricsv1connect"
	metricsHandler "github.com/Azure/adx-mon/collector/metrics"
	"github.com/Azure/adx-mon/collector/otlp"
	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/http"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/remote"
	"github.com/Azure/adx-mon/pkg/service"
	"github.com/Azure/adx-mon/storage"
	"github.com/Azure/adx-mon/transform"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Service struct {
	opts *ServiceOpts

	cancel context.CancelFunc

	// metricsSvc is the internal metrics component for collector specific metrics.
	metricsSvc metrics.Service

	// workerSvcs are the collection services that can be opened and closed.
	workerSvcs []service.Component

	// httpServers is the shared HTTP servers for the collector.  The logs and metrics services are registered with these servers.
	httpServers []*http.HttpServer

	// store is the local WAL store.
	store storage.Store

	// scraper is the metrics scraper that scrapes metrics from the local node.
	scraper *Scraper

	// httpHandlers are the write endpoints that receive from Prometheus and Otel clients over HTTP.
	httpHandlers []*http.HttpHandler

	// grpcHandlers are the write endpoints that receive calls over GRPC
	grpcHandlers []*http.GRPCHandler

	// batcher is the component that batches metrics and logs for transferring to ingestor.
	batcher cluster.Batcher

	// replicator is the component that replicates metrics and logs to the ingestor.
	replicator service.Component
}

type ServiceOpts struct {
	ListenAddr string
	NodeName   string
	Endpoint   string

	// LogCollectionHandlers is the list of log collection handlers
	LogCollectionHandlers []LogCollectorOpts
	// HttpLogCollectionHandlers is the list of log collection handlers with HTTP endpoints
	HttpLogCollectorOpts []HttpLogCollectorOpts
	// PromMetricsHandlers is the list of prom-remote handlers
	PromMetricsHandlers []PrometheusRemoteWriteHandlerOpts
	// OtlpMetricsHandlers is the list of oltp metrics handlers
	OtlpMetricsHandlers []OtlpMetricsHandlerOpts
	// Scraper is the options for the prom scraper
	Scraper *ScraperOpts

	// Labels to lift to columns
	LiftLabels []string

	AddAttributes  map[string]string
	LiftAttributes []string
	LiftResources  []string

	// InsecureSkipVerify skips the verification of the remote write endpoint certificate chain and host name.
	InsecureSkipVerify bool

	TLSCertFile string
	TLSKeyFile  string

	// MaxBatchSize is the maximum number of samples to send in a single batch.
	MaxBatchSize int

	// MaxSegmentAge is the maximum time allowed before a segment is rolled over.
	MaxSegmentAge time.Duration

	// MaxSegmentSize is the maximum size allowed for a segment before it is rolled over.
	MaxSegmentSize int64

	// MaxDiskUsage is the max size in bytes to use for segment store.  If this value is exceeded, writes
	// will be rejected until space is freed.  A value of 0 means no max usage.
	MaxDiskUsage int64

	// MaxSegmentCount is the maximum number of segments files allowed on disk before signaling back-pressure.
	MaxSegmentCount int64

	// StorageDir is the directory where the WAL will be stored
	StorageDir string

	// EnablePprof enables pprof endpoints.
	EnablePprof bool

	// Region is a location identifier
	Region string

	MaxConnections int

	// WALFlushInterval is the interval at which the WAL segment is flushed to disk.  A higher value results in
	// better disk IO and less CPU usage but has a greater risk of data loss in the event of a crash.  A lower
	// value results in more frequent disk IO and higher CPU usage but less data loss in the event of a crash.
	// The default is 100ms and the value must be greater than 0.
	WALFlushInterval time.Duration

	// MaxTransferConcurrency is the maximum number of concurrent transfers to the ingestor.
	MaxTransferConcurrency int

	// MaxBatchSegments is the maximum number of segments to include when transferring segments in a batch.  The segments
	// are merged into a new segment.  A higher number takes longer to combine on the sending size and increases the
	// size of segments on the receiving size.  A lower number creates more batches and high remote transfer calls.  If
	// not specified, the default is 25.
	MaxBatchSegments int

	// DisableGzip disables gzip compression for the transfer endpoint.
	DisableGzip bool
}

type OtlpMetricsHandlerOpts struct {
	// Optional. Path is the path where the OTLP/HTTP handler will be registered.
	Path string
	// Optional. GrpcPort is the port where the metrics OTLP/GRPC handler will listen.
	GrpcPort int

	MetricOpts MetricsHandlerOpts
}

type PrometheusRemoteWriteHandlerOpts struct {
	// Path is the path where the handler will be registered.
	Path string

	MetricOpts MetricsHandlerOpts
}

type MetricsHandlerOpts struct {
	AddLabels map[string]string

	// DropLabels is a map of metric names regexes to label name regexes.  When both match, the label will be dropped.
	DropLabels map[*regexp.Regexp]*regexp.Regexp

	// DropMetrics is a slice of regexes that drops metrics when the metric name matches.  The metric name format
	// should match the Prometheus naming style before the metric is translated to a Kusto table name.
	DropMetrics []*regexp.Regexp

	KeepMetrics []*regexp.Regexp

	KeepMetricsLabelValues map[*regexp.Regexp]*regexp.Regexp

	// DisableMetricsForwarding disables the forwarding of metrics to the remote write endpoint.
	DisableMetricsForwarding bool
	DefaultDropMetrics       bool

	RemoteWriteClients []remote.RemoteWriteClient
}

func (o MetricsHandlerOpts) RequestTransformer() *transform.RequestTransformer {
	return &transform.RequestTransformer{
		AddLabels:                 o.AddLabels,
		DropLabels:                o.DropLabels,
		DropMetrics:               o.DropMetrics,
		KeepMetrics:               o.KeepMetrics,
		KeepMetricsWithLabelValue: o.KeepMetricsLabelValues,
		DefaultDropMetrics:        o.DefaultDropMetrics,
	}
}

func NewService(opts *ServiceOpts) (*Service, error) {
	maxSegmentAge := 30 * time.Second
	if opts.MaxSegmentAge.Seconds() > 0 {
		maxSegmentAge = opts.MaxSegmentAge
	}

	maxSegmentSize := int64(1024 * 1024)
	if opts.MaxSegmentSize > 0 {
		maxSegmentSize = opts.MaxSegmentSize
	}

	maxSegmentCount := int64(10000)
	if opts.MaxSegmentCount > 0 {
		maxSegmentCount = opts.MaxSegmentCount
	}

	maxDiskUsage := int64(10 * 1024 * 1024 * 1024) // 10 GB
	if opts.MaxDiskUsage > 0 {
		maxDiskUsage = opts.MaxDiskUsage
	}

	health := cluster.NewHealth(cluster.HealthOpts{
		UnhealthyTimeout: time.Minute,
		MaxSegmentCount:  maxSegmentCount,
		MaxDiskUsage:     maxDiskUsage,
	})

	store := storage.NewLocalStore(storage.StoreOpts{
		StorageDir:       opts.StorageDir,
		SegmentMaxAge:    maxSegmentAge,
		SegmentMaxSize:   maxSegmentSize,
		MaxDiskUsage:     maxDiskUsage,
		LiftedLabels:     opts.LiftLabels,
		LiftedAttributes: opts.LiftAttributes,
		LiftedResources:  opts.LiftResources,
		WALFlushInterval: opts.WALFlushInterval,
	})

	var httpHandlers []*http.HttpHandler
	var grpcHandlers []*http.GRPCHandler
	workerSvcs := []service.Component{}

	for _, handlerOpts := range opts.PromMetricsHandlers {
		metricsProxySvc := metricsHandler.NewHandler(metricsHandler.HandlerOpts{
			Path:               handlerOpts.Path,
			RequestTransformer: handlerOpts.MetricOpts.RequestTransformer(),
			RequestWriters:     append(handlerOpts.MetricOpts.RemoteWriteClients, &StoreRequestWriter{store}),
			HealthChecker:      health,
		})
		httpHandlers = append(httpHandlers, &http.HttpHandler{
			Path:    handlerOpts.Path,
			Handler: metricsProxySvc.HandleReceive,
		})
	}

	for _, handlerOpts := range opts.OtlpMetricsHandlers {
		writer := otlp.NewOltpMetricWriter(otlp.OltpMetricWriterOpts{
			RequestTransformer:       handlerOpts.MetricOpts.RequestTransformer(),
			Clients:                  append(handlerOpts.MetricOpts.RemoteWriteClients, &StoreRemoteClient{store}),
			MaxBatchSize:             opts.MaxBatchSize,
			DisableMetricsForwarding: handlerOpts.MetricOpts.DisableMetricsForwarding,
			HealthChecker:            health,
		})
		oltpMetricsService := otlp.NewMetricsService(writer, handlerOpts.Path, handlerOpts.GrpcPort)
		if handlerOpts.Path != "" {
			httpHandlers = append(httpHandlers, &http.HttpHandler{
				Path:    handlerOpts.Path,
				Handler: oltpMetricsService.Handler,
			})
		}

		if handlerOpts.GrpcPort > 0 {
			path, handler := metricsv1connect.NewMetricsServiceHandler(oltpMetricsService)

			grpcHandlers = append(grpcHandlers, &http.GRPCHandler{
				Port:    handlerOpts.GrpcPort,
				Path:    path,
				Handler: handler,
			})
		}
	}

	var (
		replicator    service.Component
		transferQueue chan *cluster.Batch
		partitioner   cluster.MetricPartitioner
	)
	if opts.Endpoint != "" {
		// This is a static partitioner that forces all entries to be assigned to the remote endpoint.
		partitioner = remotePartitioner{
			host: "remote",
			addr: opts.Endpoint,
		}

		r, err := cluster.NewReplicator(cluster.ReplicatorOpts{
			Hostname:               opts.NodeName,
			Partitioner:            partitioner,
			Health:                 health,
			SegmentRemover:         store,
			InsecureSkipVerify:     opts.InsecureSkipVerify,
			MaxTransferConcurrency: opts.MaxTransferConcurrency,
			DisableGzip:            opts.DisableGzip,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create replicator: %w", err)
		}
		transferQueue = r.TransferQueue()
		replicator = r
	} else {
		partitioner = remotePartitioner{
			host: "remote",
			addr: "http://remotehost:1234",
		}

		r := cluster.NewFakeReplicator()
		transferQueue = r.TransferQueue()
		replicator = r
	}

	batcher := cluster.NewBatcher(cluster.BatcherOpts{
		StorageDir:         opts.StorageDir,
		MaxSegmentAge:      time.Minute,
		Partitioner:        partitioner,
		Segmenter:          store.Index(),
		MinUploadSize:      4 * 1024 * 1024,
		MaxBatchSegments:   opts.MaxBatchSegments,
		UploadQueue:        transferQueue,
		TransferQueue:      transferQueue,
		PeerHealthReporter: health,
	})

	health.QueueSizer = batcher

	var scraper *Scraper
	if opts.Scraper != nil {
		scraperOpts := opts.Scraper
		scraperOpts.RemoteClients = append(scraperOpts.RemoteClients, &StoreRemoteClient{store})
		scraperOpts.HealthChecker = health

		scraper = NewScraper(opts.Scraper)
	}

	for _, handlerOpts := range opts.LogCollectionHandlers {
		svc, err := handlerOpts.Create(store)
		if err != nil {
			return nil, fmt.Errorf("failed to create log collection service: %w", err)
		}

		workerSvcs = append(workerSvcs, svc)
	}

	for _, handlerOpts := range opts.HttpLogCollectorOpts {
		svc, httpHandler, err := handlerOpts.CreateHTTPSvc(store, health)
		if err != nil {
			return nil, fmt.Errorf("failed to create log collection service: %w", err)
		}
		workerSvcs = append(workerSvcs, svc)
		httpHandlers = append(httpHandlers, httpHandler)
	}

	svc := &Service{
		opts: opts,
		metricsSvc: metrics.NewService(metrics.ServiceOpts{
			PeerHealthReport: health,
		}),
		store:        store,
		scraper:      scraper,
		workerSvcs:   workerSvcs,
		httpHandlers: httpHandlers,
		grpcHandlers: grpcHandlers,
		batcher:      batcher,
		replicator:   replicator,
	}

	return svc, nil
}

func (s *Service) Open(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)

	if err := s.store.Open(ctx); err != nil {
		return fmt.Errorf("failed to open wal store: %w", err)
	}

	if err := s.metricsSvc.Open(ctx); err != nil {
		return fmt.Errorf("failed to open metrics service: %w", err)
	}

	for _, workerSvc := range s.workerSvcs {
		if err := workerSvc.Open(ctx); err != nil {
			return fmt.Errorf("failed to open worker service: %w", err)
		}
	}

	if err := s.replicator.Open(ctx); err != nil {
		return err
	}

	if err := s.batcher.Open(ctx); err != nil {
		return err
	}

	if s.scraper != nil {
		if err := s.scraper.Open(ctx); err != nil {
			return err
		}
	}

	listenerFunc := plaintextListenerFunc()
	if s.opts.TLSCertFile != "" && s.opts.TLSKeyFile != "" {
		logger.Infof("TLS enabled for listeners")
		cert, err := tls.LoadX509KeyPair(s.opts.TLSCertFile, s.opts.TLSKeyFile)
		if err != nil {
			return fmt.Errorf("failed to load cert and key: %w", err)
		}
		listenerFunc = tlsListenerFunc(cert)
	}

	s.httpServers = []*http.HttpServer{}

	listener, err := listenerFunc(s.opts.ListenAddr)
	if err != nil {
		return err
	}

	opts := &http.ServerOpts{
		MaxConns:     s.opts.MaxConnections,
		WriteTimeout: 30 * time.Second,
		Listener:     listener,
	}

	primaryHttp := http.NewServer(opts)

	primaryHttp.RegisterHandler("/metrics", promhttp.Handler())
	if s.opts.EnablePprof {
		opts.WriteTimeout = 60 * time.Second
		primaryHttp.RegisterHandlerFunc("/debug/pprof/", pprof.Index)
		primaryHttp.RegisterHandlerFunc("/debug/pprof/cmdline", pprof.Cmdline)
		primaryHttp.RegisterHandlerFunc("/debug/pprof/profile", pprof.Profile)
		primaryHttp.RegisterHandlerFunc("/debug/pprof/symbol", pprof.Symbol)
		primaryHttp.RegisterHandlerFunc("/debug/pprof/trace", pprof.Trace)
	}

	for _, handler := range s.httpHandlers {
		primaryHttp.RegisterHandlerFunc(handler.Path, handler.Handler)
	}
	s.httpServers = append(s.httpServers, primaryHttp)

	for _, handler := range s.grpcHandlers {
		listener, err := listenerFunc(fmt.Sprintf(":%d", handler.Port))
		if err != nil {
			return err
		}
		server := http.NewServer(&http.ServerOpts{
			MaxConns: s.opts.MaxConnections,
			Listener: listener,
		})
		server.RegisterHandler(handler.Path, handler.Handler)
		s.httpServers = append(s.httpServers, server)
	}

	for _, httpServer := range s.httpServers {
		if err := httpServer.Open(ctx); err != nil {
			return err
		}
		logger.Infof("Started %s", httpServer)
	}

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				metrics.CollectorHealthCheck.WithLabelValues(s.opts.Region).Set(1)
			}
		}
	}()

	return nil
}

func (s *Service) Close() error {
	if s.scraper != nil {
		s.scraper.Close()
	}

	s.metricsSvc.Close()
	for _, workerSvc := range s.workerSvcs {
		workerSvc.Close()
	}
	s.cancel()
	for _, httpServer := range s.httpServers {
		httpServer.Close()
	}
	s.batcher.Close()
	s.replicator.Close()
	s.store.Close()
	return nil
}

func tlsListenerFunc(cert tls.Certificate) func(addr string) (net.Listener, error) {
	return func(addr string) (net.Listener, error) {
		listener, err := tls.Listen("tcp", addr, &tls.Config{
			Certificates: []tls.Certificate{cert},
		})
		if err != nil {
			return listener, fmt.Errorf("failed to create listener: %w", err)
		}

		return listener, nil
	}
}

func plaintextListenerFunc() func(addr string) (net.Listener, error) {
	return func(addr string) (net.Listener, error) {
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return listener, fmt.Errorf("failed to create listener: %w", err)
		}

		return listener, nil
	}
}

// remotePartitioner is a Partitioner that always returns the same owner that forces a remove transfer.
type remotePartitioner struct {
	host, addr string
}

func (f remotePartitioner) Owner(bytes []byte) (string, string) {
	return f.host, f.addr
}

type StoreRequestWriter struct {
	store storage.Store
}

func (s *StoreRequestWriter) Write(ctx context.Context, req *prompb.WriteRequest) error {
	return s.store.WriteTimeSeries(ctx, req.Timeseries)
}

func (s *StoreRequestWriter) CloseIdleConnections() {
}

type StoreRemoteClient struct {
	store storage.Store
}

func (s *StoreRemoteClient) Write(ctx context.Context, wr *prompb.WriteRequest) error {
	return s.store.WriteTimeSeries(ctx, wr.Timeseries)
}

func (s *StoreRemoteClient) CloseIdleConnections() {
}
