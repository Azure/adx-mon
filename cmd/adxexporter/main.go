package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"

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
				Name:     "otlp-endpoint",
				Usage:    "OTLP/HTTP endpoint URL for pushing metrics (required)",
				Required: true,
			},
			&cli.StringSliceFlag{
				Name:  "add-resource-attributes",
				Usage: "Key/value pairs of resource attributes to add to all exported metrics. Format: <key>=<value>. These are merged with cluster-labels (explicit attributes take precedence).",
			},
			&cli.StringFlag{
				Name:  "health-probe-port",
				Usage: "Address and port for health probe endpoints",
				Value: ":8081",
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

	addResourceAttributes, err := parseKeyValuePairs(ctx.StringSlice("add-resource-attributes"))
	if err != nil {
		return fmt.Errorf("invalid add-resource-attributes: %w", err)
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

	// Let controller-runtime handle signals
	svcCtx := ctrl.SetupSignalHandler()

	// Set the controller-runtime logger to use our logger
	log.SetLogger(logger.AsLogr())

	// Get config and create manager
	cfg := ctrl.GetConfigOrDie()

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: ctx.String("health-probe-port"),
		Metrics: metricsserver.Options{
			BindAddress: "0", // Disable built-in metrics server - we push via OTLP
		},
	})
	if err != nil {
		return fmt.Errorf("unable to create manager: %w", err)
	}

	// Set up controllers
	adxexp := &adxexporter.MetricsExporterReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),

		ClusterLabels:         clusterLabels,
		KustoClusters:         kustoClusters,
		OTLPEndpoint:          ctx.String("otlp-endpoint"),
		AddResourceAttributes: addResourceAttributes,
	}
	if err = adxexp.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create adxexporter controller: %w", err)
	}

	// SummaryRule controller skeleton (no-op for now)
	sr := &adxexporter.SummaryRuleReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		ClusterLabels: clusterLabels,
		KustoClusters: kustoClusters,
	}
	if err = sr.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create summaryrule controller: %w", err)
	}

	if err := mgr.AddReadyzCheck("readyz", func(req *http.Request) error {
		if adxexp.OtlpExporter == nil {
			return fmt.Errorf("OTLP exporter not initialized")
		}
		return nil
	}); err != nil {
		return fmt.Errorf("unable to add readyz check: %w", err)
	}

	if err := mgr.AddHealthzCheck("healthz", func(req *http.Request) error {
		if adxexp.OtlpExporter == nil {
			return fmt.Errorf("OTLP exporter not initialized")
		}
		return nil
	}); err != nil {
		return fmt.Errorf("unable to add healthz check: %w", err)
	}

	// Start manager
	if err := mgr.Start(svcCtx); err != nil {
		logger.Errorf("Problem running manager: %v", err)
	}

	return nil
}

// parseClusterLabels processes --cluster-labels CLI arguments into a map
// Similar to the ingestor implementation for consistency
func parseClusterLabels(labels []string) (map[string]string, error) {
	return parseKeyValuePairs(labels)
}

// parseKeyValuePairs processes key=value CLI arguments into a map
func parseKeyValuePairs(pairs []string) (map[string]string, error) {
	result := make(map[string]string)

	for _, pair := range pairs {
		split := strings.SplitN(pair, "=", 2)
		if len(split) != 2 {
			return nil, fmt.Errorf("invalid format: %s, expected <key>=<value>", pair)
		}

		key := split[0]
		value := split[1]

		// Store the key-value pair as-is
		result[key] = value
	}

	return result, nil
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
