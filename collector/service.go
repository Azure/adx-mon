package collector

import (
	"context"
	"fmt"
	"net/http/pprof"
	_ "net/http/pprof"
	"regexp"
	"time"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/metrics/v1/metricsv1connect"
	"github.com/Azure/adx-mon/collector/otlp"
	"github.com/Azure/adx-mon/ingestor/cluster"
	metricsHandler "github.com/Azure/adx-mon/ingestor/metrics"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/ingestor/transform"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/http"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/promremote"
	"github.com/Azure/adx-mon/pkg/service"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Service struct {
	opts *ServiceOpts

	cancel context.CancelFunc

	// remoteClient is the metrics client used to send metrics to ingestor.
	remoteClient *promremote.Client

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

	AddAttributes  map[string]string
	LiftAttributes []string

	// InsecureSkipVerify skips the verification of the remote write endpoint certificate chain and host name.
	InsecureSkipVerify bool

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
		StorageDir:     opts.StorageDir,
		SegmentMaxAge:  maxSegmentAge,
		SegmentMaxSize: maxSegmentSize,
		MaxDiskUsage:   opts.MaxDiskUsage,
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
			Timeout:               10 * time.Second,
			InsecureSkipVerify:    opts.InsecureSkipVerify,
			Close:                 false,
			MaxIdleConnsPerHost:   1,
			MaxConnsPerHost:       5,
			MaxIdleConns:          1,
			ResponseHeaderTimeout: 10 * time.Second,
			DisableHTTP2:          true,
			DisableKeepAlives:     true,
		})
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus remote client: %w", err)
	}

	var metricHttpHandlers []*http.HttpHandler
	var grpcHandlers []*http.GRPCHandler
	for _, handlerOpts := range opts.PromMetricsHandlers {
		metricsProxySvc := metricsHandler.NewHandler(metricsHandler.HandlerOpts{
			Path:               handlerOpts.Path,
			RequestTransformer: handlerOpts.MetricOpts.RequestTransformer(),
			RequestWriter: &promremote.RemoteWriteProxy{
				Client:                   remoteClient,
				Endpoints:                opts.Endpoints,
				MaxBatchSize:             opts.MaxBatchSize,
				DisableMetricsForwarding: handlerOpts.MetricOpts.DisableMetricsForwarding,
			},
			HealthChecker: fakeHealthChecker{},
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
		scraperOpts.RemoteClient = remoteClient
		scraperOpts.Endpoints = opts.Endpoints

		scraper = NewScraper(opts.Scraper)
	}

	workerSvcs := []service.Component{}
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
		remoteClient: remoteClient,
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

	s.httpServers = []*http.HttpServer{}

	primaryHttp := http.NewServer(&http.ServerOpts{
		ListenAddr: s.opts.ListenAddr,
		MaxConns:   s.opts.MaxConnections,
	})

	primaryHttp.RegisterHandler("/metrics", promhttp.Handler())
	if s.opts.EnablePprof {
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
		server := http.NewServer(&http.ServerOpts{
			ListenAddr: fmt.Sprintf(":%d", handler.Port),
			MaxConns:   s.opts.MaxConnections,
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

type fakeHealthChecker struct{}

func (f fakeHealthChecker) IsPeerHealthy(peer string) bool { return true }
func (f fakeHealthChecker) SetPeerUnhealthy(peer string)   {}
func (f fakeHealthChecker) SetPeerHealthy(peer string)     {}
func (f fakeHealthChecker) TransferQueueSize() int         { return 0 }
func (f fakeHealthChecker) UploadQueueSize() int           { return 0 }
func (f fakeHealthChecker) SegmentsTotal() int64           { return 0 }
func (f fakeHealthChecker) SegmentsSize() int64            { return 0 }
func (f fakeHealthChecker) IsHealthy() bool                { return true }

// remotePartitioner is a Partitioner that always returns the same owner that forces a remove transfer.
type remotePartitioner struct {
	host, addr string
}

func (f remotePartitioner) Owner(bytes []byte) (string, string) {
	return f.host, f.addr
}
