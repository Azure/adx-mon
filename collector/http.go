package collector

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"

	"github.com/Azure/adx-mon/collector/otlp"
	metricsHandler "github.com/Azure/adx-mon/ingestor/metrics"
	"github.com/Azure/adx-mon/pkg/promremote"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type HttpServerOpts struct {
	ListenAddr         string
	InsecureSkipVerify bool

	Endpoints                []string
	MaxBatchSize             int
	DisableMetricsForwarding bool

	RemoteWriteClient *promremote.Client

	// AddLabels is a map of label names and values.  These labels will be added to all metrics.
	AddLabels map[string]string

	// DropLabels is a map of metric names regexes to label name regexes.  When both match, the label will be dropped.
	DropLabels map[*regexp.Regexp]*regexp.Regexp

	// DropMetrics is a slice of regexes that drops metrics when the metric name matches.  The metric name format
	// should match the Prometheus naming style before the metric is translated to a Kusto table name.
	DropMetrics []*regexp.Regexp

	AddAttributes  map[string]string
	LiftAttributes []string
}

type HttpServer struct {
	mux  *http.ServeMux
	opts *HttpServerOpts
	srv  *http.Server

	cancelFn context.CancelFunc
}

func NewHttpServer(opts *HttpServerOpts) *HttpServer {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/remote_write", metricsHandler.NewHandler(metricsHandler.HandlerOpts{
		DropLabels:  opts.DropLabels,
		DropMetrics: opts.DropMetrics,
		AddLabels:   opts.AddLabels,
		RequestWriter: &promremote.RemoteWriteProxy{
			Client:                   opts.RemoteWriteClient,
			Endpoints:                opts.Endpoints,
			MaxBatchSize:             opts.MaxBatchSize,
			DisableMetricsForwarding: opts.DisableMetricsForwarding,
		},
		HealthChecker: fakeHealthChecker{},
	}))
	return &HttpServer{
		opts: opts,
		mux:  mux,
	}
}

func (s *HttpServer) Open(ctx context.Context) error {
	s.mux.Handle("/logs", otlp.LogsProxyHandler(ctx, s.opts.Endpoints, s.opts.InsecureSkipVerify, s.opts.AddAttributes, s.opts.LiftAttributes))
	s.mux.Handle("/v1/logs", otlp.LogsTransferHandler(ctx, s.opts.Endpoints, s.opts.InsecureSkipVerify, s.opts.AddAttributes))
	s.srv = &http.Server{Addr: s.opts.ListenAddr, Handler: s.mux}

	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}()
	return nil
}

func (s *HttpServer) Close() error {
	return s.srv.Shutdown(context.Background())
}
