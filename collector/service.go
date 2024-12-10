package collector

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http/pprof"
	_ "net/http/pprof"
	"regexp"
	"time"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/metrics/v1/metricsv1connect"
	"github.com/Azure/adx-mon/collector/otlp"
	"github.com/Azure/adx-mon/ingestor/cluster"
	metricsHandler "github.com/Azure/adx-mon/ingestor/metrics"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/http"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/promremote"
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

	// otelLogsSvc is the OpenTelemetry logs service that receives OTLP/HTTP Logs requests and stores
	// in the local WAL.
	otelLogsSvc *otlp.LogsService

	// otelProxySvc is the OpenTelemetry logs proxy service that receives OTLP/HTTP Logs requests and
	// forwards them via OTLP/GRPC to ingestor.
	otelProxySvc *otlp.LogsProxyService

	// httpHandlers are the write endpoints that receive metrics from Prometheus and Otel clients over HTTP.
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
	Endpoints  []string

	// LogCollectionHandlers is the list of log collection handlers
	LogCollectionHandlers []LogCollectorOpts
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
}

func (o MetricsHandlerOpts) RequestTransformer() *transform.RequestTransformer {
	return &transform.RequestTransformer{
		AddLabels:   o.AddLabels,
		DropLabels:  o.DropLabels,
		DropMetrics: o.DropMetrics,
		KeepMetrics: o.KeepMetrics,
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

	store := storage.NewLocalStore(storage.StoreOpts{
		StorageDir:       opts.StorageDir,
		SegmentMaxAge:    maxSegmentAge,
		SegmentMaxSize:   maxSegmentSize,
		MaxDiskUsage:     opts.MaxDiskUsage,
		LiftedLabels:     opts.LiftLabels,
		LiftedAttributes: opts.LiftAttributes,
		LiftedResources:  opts.LiftResources,
		WALFlushInterval: opts.WALFlushInterval,
	})

	logsSvc := otlp.NewLogsService(otlp.LogsServiceOpts{
		Store:         store,
		AddAttributes: opts.AddAttributes,
	})

	logsProxySvc := otlp.NewLogsProxyService(otlp.LogsProxyServiceOpts{
		LiftAttributes:     opts.LiftAttributes,
		AddAttributes:      opts.AddAttributes,
		Endpoints:          opts.Endpoints,
		InsecureSkipVerify: opts.InsecureSkipVerify,
	})

	remoteClient, err := promremote.NewClient(
		promremote.ClientOpts{
			Timeout:               20 * time.Second,
			InsecureSkipVerify:    opts.InsecureSkipVerify,
			Close:                 false,
			MaxIdleConnsPerHost:   1,
			MaxConnsPerHost:       5,
			MaxIdleConns:          1,
			ResponseHeaderTimeout: 20 * time.Second,
			DisableHTTP2:          true,
			DisableKeepAlives:     true,
		})
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus remote client: %w", err)
	}

	var metricHttpHandlers []*http.HttpHandler
	var grpcHandlers []*http.GRPCHandler
	workerSvcs := []service.Component{}
	for _, handlerOpts := range opts.PromMetricsHandlers {
		metricsProxySvc := metricsHandler.NewHandler(metricsHandler.HandlerOpts{
			Path:               handlerOpts.Path,
			RequestTransformer: handlerOpts.MetricOpts.RequestTransformer(),
			RequestWriter:      &StoreRequestWriter{store},
			HealthChecker:      fakeHealthChecker{},
		})
		metricHttpHandlers = append(metricHttpHandlers, &http.HttpHandler{
			Path:    handlerOpts.Path,
			Handler: metricsProxySvc.HandleReceive,
		})
	}

	for _, handlerOpts := range opts.OtlpMetricsHandlers {
		writer := otlp.NewOltpMetricWriter(otlp.OltpMetricWriterOpts{
			RequestTransformer:       handlerOpts.MetricOpts.RequestTransformer(),
			Client:                   remoteClient,
			Endpoints:                opts.Endpoints,
			MaxBatchSize:             opts.MaxBatchSize,
			DisableMetricsForwarding: handlerOpts.MetricOpts.DisableMetricsForwarding,
		})
		oltpMetricsService := otlp.NewMetricsService(writer, handlerOpts.Path, handlerOpts.GrpcPort)
		if handlerOpts.Path != "" {
			metricHttpHandlers = append(metricHttpHandlers, &http.HttpHandler{
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
	if len(opts.Endpoints) > 0 {
		// This is a static partitioner that forces all entries to be assigned to the remote endpoint.
		partitioner = remotePartitioner{
			host: "remote",
			addr: opts.Endpoints[0],
		}

		r, err := cluster.NewReplicator(cluster.ReplicatorOpts{
			Hostname:           opts.NodeName,
			Partitioner:        partitioner,
			Health:             fakeHealthChecker{},
			SegmentRemover:     store,
			InsecureSkipVerify: opts.InsecureSkipVerify,
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
		UploadQueue:        transferQueue,
		TransferQueue:      transferQueue,
		PeerHealthReporter: fakeHealthChecker{},
	})

	var scraper *Scraper
	if opts.Scraper != nil {
		scraperOpts := opts.Scraper
		scraperOpts.RemoteClient = &StoreRemoteClient{store}
		scraperOpts.Endpoints = opts.Endpoints

		scraper = NewScraper(opts.Scraper)
	}

	for _, handlerOpts := range opts.LogCollectionHandlers {
		svc, err := handlerOpts.Create(store)
		if err != nil {
			return nil, fmt.Errorf("failed to create log collection service: %w", err)
		}

		workerSvcs = append(workerSvcs, svc)
	}

	svc := &Service{
		opts: opts,
		metricsSvc: metrics.NewService(metrics.ServiceOpts{
			PeerHealthReport: fakeHealthChecker{},
		}),
		store:        store,
		scraper:      scraper,
		workerSvcs:   workerSvcs,
		otelLogsSvc:  logsSvc,
		otelProxySvc: logsProxySvc,
		httpHandlers: metricHttpHandlers,
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

	if err := s.otelLogsSvc.Open(ctx); err != nil {
		return err
	}

	if err := s.otelProxySvc.Open(ctx); err != nil {
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

	primaryHttp.RegisterHandlerFunc("/v1/logs", s.otelLogsSvc.Handler)
	primaryHttp.RegisterHandlerFunc("/logs", s.otelProxySvc.Handler)

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
	if s.otelProxySvc != nil {
		s.otelProxySvc.Close()
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

type fakeHealthChecker struct{}

func (f fakeHealthChecker) IsPeerHealthy(peer string) bool { return true }
func (f fakeHealthChecker) SetPeerUnhealthy(peer string)   {}
func (f fakeHealthChecker) SetPeerHealthy(peer string)     {}
func (f fakeHealthChecker) TransferQueueSize() int         { return 0 }
func (f fakeHealthChecker) UploadQueueSize() int           { return 0 }
func (f fakeHealthChecker) SegmentsTotal() int64           { return 0 }
func (f fakeHealthChecker) SegmentsSize() int64            { return 0 }
func (f fakeHealthChecker) IsHealthy() bool                { return true }
func (f fakeHealthChecker) UnhealthyReason() string        { return "" }

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

type StoreRemoteClient struct {
	store storage.Store
}

func (s *StoreRemoteClient) Write(ctx context.Context, endpoint string, wr *prompb.WriteRequest) error {
	return s.store.WriteTimeSeries(ctx, wr.Timeseries)
}

func (s *StoreRemoteClient) CloseIdleConnections() {
}
