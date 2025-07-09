package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Azure/adx-mon/adxexporter"
	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/version"
	"github.com/urfave/cli/v2"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func main() {
	app := &cli.App{
		Name:  "adxexporter",
		Usage: "adx-mon metrics exporter",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:  "cluster-labels",
				Usage: "Labels used to identify and distinguish adxexporter clusters. Format: <key>=<value>",
			},
			&cli.StringSliceFlag{
				Name:  "kusto-endpoints",
				Usage: "Kusto endpoint in the format of <db>=<endpoint> for query execution",
			},
			&cli.StringFlag{
				Name:  "otlp-endpoint",
				Usage: "OTLP endpoint URL for direct push mode (Phase 2)",
			},
			&cli.BoolFlag{
				Name:  "enable-metrics-endpoint",
				Usage: "Enable the Prometheus metrics HTTP server",
				Value: false,
			},
			&cli.StringFlag{
				Name:  "metrics-port",
				Usage: "Address and port for the health checks and metrics server",
				Value: ":8080",
			},
			&cli.StringFlag{
				Name:  "metrics-path",
				Usage: "HTTP path for metrics endpoint",
				Value: "/metrics",
			},
		},
		Action:  realMain,
		Version: version.String(),
	}

	if err := app.Run(os.Args); err != nil {
		logger.Fatal(err.Error())
	}
}

func realMain(ctx *cli.Context) error {
	clusterLabels, err := parseClusterLabels(ctx.StringSlice("cluster-labels"))
	if err != nil {
		return err
	}

	kustoClusters, err := parseKustoEndpoints(ctx.StringSlice("kusto-endpoints"))
	if err != nil {
		return err
	}

	// Build scheme
	scheme := clientgoscheme.Scheme
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return fmt.Errorf("unable to add client-go scheme: %w", err)
	}
	if err := adxmonv1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("unable to add adxmonv1 scheme: %w", err)
	}

	// Create cancellable context
	svcCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sc
		logger.Infof("Received signal %s, initiating graceful shutdown...", sig.String())
		cancel()
	}()

	// Set the controller-runtime logger to use our logger
	log.SetLogger(logger.AsLogr())

	// Get config and create manager
	cfg := ctrl.GetConfigOrDie()

	// Configure server address - use same port for health checks and metrics
	var serverBindAddress string
	if ctx.Bool("enable-metrics-endpoint") {
		serverBindAddress = ctx.String("metrics-port")
	} else {
		serverBindAddress = "0" // Disable server
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: ":8081", // Port for health endpoints
		Metrics: metricsserver.Options{
			BindAddress: serverBindAddress,
		},
	})
	if err != nil {
		return fmt.Errorf("unable to create manager: %w", err)
	}

	// Set up controller
	adxexp := &adxexporter.MetricsExporterReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),

		ClusterLabels:         clusterLabels,
		KustoClusters:         kustoClusters,
		OTLPEndpoint:          ctx.String("otlp-endpoint"),
		EnableMetricsEndpoint: ctx.Bool("enable-metrics-endpoint"),
		MetricsPort:           ctx.String("metrics-port"),
		MetricsPath:           ctx.String("metrics-path"),
	}
	if err = adxexp.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create adxexporter controller: %w", err)
	}

	if err := mgr.AddReadyzCheck("metrics-ready", func(req *http.Request) error {
		if !adxexp.EnableMetricsEndpoint {
			return nil // Always ready if metrics are disabled
		}

		if adxexp.Meter == nil {
			return fmt.Errorf("metrics not ready")
		}

		return nil
	}); err != nil {
		return fmt.Errorf("unable to add readyz check: %w", err)
	}

	if err := mgr.AddHealthzCheck("healthz", func(req *http.Request) error {
		if adxexp.Meter == nil {
			return fmt.Errorf("metrics not ready")
		}
		return nil
	}); err != nil {
		return fmt.Errorf("unable to add healthz check: %w", err)
	}

	// Start manager
	go func() {
		if err := mgr.Start(svcCtx); err != nil {
			logger.Errorf("Problem running manager: %v", err)
			cancel()
		}
	}()

	<-svcCtx.Done()
	return nil
}

// parseClusterLabels processes --cluster-labels CLI arguments into a map
// Similar to the ingestor implementation for consistency
func parseClusterLabels(labels []string) (map[string]string, error) {
	clusterLabels := make(map[string]string)

	for _, label := range labels {
		split := strings.SplitN(label, "=", 2)
		if len(split) != 2 {
			return nil, fmt.Errorf("invalid cluster label format: %s, expected <key>=<value>", label)
		}

		key := split[0]
		value := split[1]

		// Store the key-value pair as-is for criteria matching
		clusterLabels[key] = value
	}

	return clusterLabels, nil
}

// parseKustoEndpoints processes --kusto-endpoints CLI arguments into a map
// Following the same pattern as the ingestor implementation
func parseKustoEndpoints(endpoints []string) (map[string]string, error) {
	kustoClusters := make(map[string]string)

	for _, endpoint := range endpoints {
		if !strings.Contains(endpoint, "=") {
			return nil, fmt.Errorf("invalid kusto endpoint: %s, expected <database>=<endpoint>", endpoint)
		}

		split := strings.Split(endpoint, "=")
		database := split[0]
		addr := split[1]

		if database == "" {
			return nil, fmt.Errorf("database name is required in kusto endpoint: %s", endpoint)
		}

		if addr == "" {
			return nil, fmt.Errorf("endpoint address is required in kusto endpoint: %s", endpoint)
		}

		kustoClusters[database] = addr
	}

	return kustoClusters, nil
}
