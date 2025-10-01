package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/urfave/cli/v2"
	"k8s.io/client-go/kubernetes"

	"github.com/Azure/adx-mon/cmd/collector/config"
	"github.com/Azure/adx-mon/collector"
	"github.com/Azure/adx-mon/collector/logs"
	"github.com/Azure/adx-mon/collector/logs/engine"
	"github.com/Azure/adx-mon/collector/logs/sinks"
	"github.com/Azure/adx-mon/collector/logs/sources/journal"
	"github.com/Azure/adx-mon/collector/logs/sources/kernel"
	"github.com/Azure/adx-mon/collector/logs/sources/tail"
	"github.com/Azure/adx-mon/collector/logs/transforms"
	"github.com/Azure/adx-mon/collector/logs/transforms/parser"
	"github.com/Azure/adx-mon/collector/logs/transforms/plugin/addattributes"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/collector/otlp"
	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/http"
	"github.com/Azure/adx-mon/pkg/k8s"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/remote"
	"github.com/Azure/adx-mon/pkg/version"
	"github.com/Azure/adx-mon/schema"
	"github.com/Azure/adx-mon/storage"
)

func main() {
	app := &cli.App{
		Name:      "collector",
		Usage:     "adx-mon metrics collector",
		UsageText: ``,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "hostname", Usage: "Hostname filter override"},
			&cli.StringFlag{Name: "config", Usage: "Config file path"},
			&cli.StringFlag{
				Name:        "storage-backend",
				Usage:       "Override storage backend (adx or clickhouse)",
				EnvVars:     []string{"COLLECTOR_STORAGE_BACKEND"},
				DefaultText: string(storage.BackendADX),
			},
		},

		Commands: []*cli.Command{
			{
				Name:  "config",
				Usage: "Generate a config file",
				Action: func(c *cli.Context) error {
					buf := bytes.Buffer{}
					enc := toml.NewEncoder(&buf)
					enc.SetIndentTables(true)
					if err := enc.Encode(config.DefaultConfig); err != nil {
						return err
					}

					fmt.Println(buf.String())

					return nil
				},
			},
		},

		Action: func(ctx *cli.Context) error {
			return realMain(ctx)
		},

		Version: version.String(),
	}

	if err := app.Run(os.Args); err != nil {
		logger.Fatal(err.Error())
	}
}

func realMain(ctx *cli.Context) error {
	logger.Infof("%s version:%s", os.Args[0], version.String())

	runtime.MemProfileRate = 4096
	runtime.SetBlockProfileRate(int(1 * time.Second))
	runtime.SetMutexProfileFraction(1)

	var cfg = config.DefaultConfig
	configFile := ctx.String("config")

	if configFile == "" {
		logger.Fatalf("config file is required.  Run `collector config` to generate a config file")
	}

	configBytes, err := os.ReadFile(configFile)
	if err != nil {
		return err
	}

	var fileConfig config.Config
	if err := toml.Unmarshal(configBytes, &fileConfig); err != nil {
		var derr *toml.DecodeError
		if errors.As(err, &derr) {
			fmt.Println(derr.String())
			row, col := derr.Position()
			fmt.Println("error occurred at row", row, "column", col)
		}

		return err
	}

	cfg = fileConfig
	backendOverride := ctx.String("storage-backend")
	if ctx.IsSet("storage-backend") {
		if backendOverride == "" {
			return fmt.Errorf("storage-backend cannot be empty")
		}
		cfg.StorageBackend = backendOverride
	} else if cfg.StorageBackend == "" {
		cfg.StorageBackend = config.DefaultConfig.StorageBackend
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	backend, err := storage.ParseBackend(cfg.StorageBackend)
	if err != nil {
		return err
	}
	logger.Infof("Collector storage backend: %s", backend)

	hostname := ctx.String("hostname")
	if hostname == "" {
		var err error
		hostname, err = os.Hostname()
		if err != nil {
			return fmt.Errorf("failed to get hostname: %w", err)
		}
	}

	cfg.ReplaceVariable("$(HOSTNAME)", hostname)

	var endpoint string
	if cfg.Endpoint != "" {
		endpoint = cfg.Endpoint

		u, err := url.Parse(cfg.Endpoint)
		if err != nil {
			return fmt.Errorf("failed to parse endpoint %s: %w", endpoint, err)
		}

		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("endpoint %s must be http or https", endpoint)
		}

		logger.Infof("Using remote write endpoint %s", endpoint)
	}

	if cfg.StorageDir == "" {
		logger.Fatalf("storage-dir is required")
	} else {
		logger.Infof("Using storage dir: %s", cfg.StorageDir)
	}
	sort.Slice(cfg.LiftLabels, func(i, j int) bool {
		return strings.ToLower(cfg.LiftLabels[i].Name) < strings.ToLower(cfg.LiftLabels[j].Name)
	})

	defaultMapping := schema.DefaultMetricsMapping
	var sortedLiftedLabels []string
	for _, v := range cfg.LiftLabels {
		sortedLiftedLabels = append(sortedLiftedLabels, v.Name)
		if v.ColumnName != "" {
			defaultMapping = defaultMapping.AddStringMapping(v.ColumnName)
			continue
		}
		defaultMapping = defaultMapping.AddStringMapping(v.Name)
	}

	// Update the default mapping so pooled csv encoders can use the lifted columns
	schema.DefaultMetricsMapping = defaultMapping

	metricExporterCache := make(map[string]remote.RemoteWriteClient)
	var informer *k8s.PodInformer
	var scraperOpts *collector.ScraperOpts
	if cfg.PrometheusScrape != nil {
		informer, err = getInformer(cfg.Kubeconfig, hostname, informer)
		if err != nil {
			return fmt.Errorf("failed to get informer for prometheus scrape: %w", err)
		}

		addLabels := mergeMaps(cfg.AddLabels, cfg.PrometheusScrape.AddLabels)
		addLabels["adxmon_database"] = cfg.PrometheusScrape.Database

		var staticTargets []collector.ScrapeTarget
		for _, target := range cfg.PrometheusScrape.StaticScrapeTarget {
			if match, err := regexp.MatchString(target.HostRegex, hostname); err != nil {
				return fmt.Errorf("failed to match hostname %s with regex %s: %w", hostname, target.HostRegex, err)
			} else if !match {
				continue
			}

			staticTargets = append(staticTargets, collector.ScrapeTarget{
				Static:    true,
				Addr:      target.URL,
				Namespace: target.Namespace,
				Pod:       target.Pod,
				Container: target.Container,
			})
		}

		dropLabels := make(map[*regexp.Regexp]*regexp.Regexp)
		for k, v := range mergeMaps(cfg.DropLabels, cfg.PrometheusScrape.DropLabels) {
			metricRegex, err := regexp.Compile(k)
			if err != nil {
				logger.Fatalf("invalid metric regex: %s", err)
			}

			labelRegex, err := regexp.Compile(v)
			if err != nil {
				logger.Fatalf("invalid label regex: %s", err)
			}

			dropLabels[metricRegex] = labelRegex
		}

		dropMetrics := []*regexp.Regexp{}
		for _, v := range unionSlice(cfg.DropMetrics, cfg.PrometheusScrape.DropMetrics) {
			metricRegex, err := regexp.Compile(v)
			if err != nil {
				logger.Fatalf("invalid metric regex: %s", err)
			}

			dropMetrics = append(dropMetrics, metricRegex)
		}

		keepMetrics := []*regexp.Regexp{}
		for _, v := range unionSlice(cfg.KeepMetrics, cfg.PrometheusScrape.KeepMetrics) {
			metricRegex, err := regexp.Compile(v)
			if err != nil {
				logger.Fatalf("invalid metric regex: %s", err)
			}

			keepMetrics = append(keepMetrics, metricRegex)
		}

		keepMetricLabelValues := make(map[*regexp.Regexp]*regexp.Regexp)
		for _, v := range append(cfg.KeepMetricsWithLabelValue, cfg.PrometheusScrape.KeepMetricsWithLabelValue...) {
			lableRe, err := regexp.Compile(v.LabelRegex)
			if err != nil {
				logger.Fatalf("invalid metric regex: %s", err)
			}

			valueRe, err := regexp.Compile(v.ValueRegex)
			if err != nil {
				logger.Fatalf("invalid label regex: %s", err)
			}

			keepMetricLabelValues[lableRe] = valueRe
		}

		var defaultDropMetrics bool
		if cfg.DefaultDropMetrics != nil {
			defaultDropMetrics = *cfg.DefaultDropMetrics
		}

		if cfg.PrometheusScrape.DefaultDropMetrics != nil {
			defaultDropMetrics = *cfg.PrometheusScrape.DefaultDropMetrics
		}

		remoteClients, err := getMetricsExporters(cfg.PrometheusScrape.Exporters, cfg.Exporters, metricExporterCache)
		if err != nil {
			return fmt.Errorf("prometheus scrape: %w", err)
		}

		scraperOpts = &collector.ScraperOpts{
			NodeName:                  hostname,
			PodInformer:               informer,
			Database:                  cfg.PrometheusScrape.Database,
			AddLabels:                 addLabels,
			DropLabels:                dropLabels,
			DropMetrics:               dropMetrics,
			KeepMetrics:               keepMetrics,
			KeepMetricsWithLabelValue: keepMetricLabelValues,
			DefaultDropMetrics:        defaultDropMetrics,
			DisableMetricsForwarding:  cfg.PrometheusScrape.DisableMetricsForwarding,
			DisableDiscovery:          cfg.PrometheusScrape.DisableDiscovery,
			ScrapeInterval:            time.Duration(cfg.PrometheusScrape.ScrapeIntervalSeconds) * time.Second,
			ScrapeTimeout:             time.Duration(cfg.PrometheusScrape.ScrapeTimeout) * time.Second,
			Targets:                   staticTargets,
			MaxBatchSize:              cfg.MaxBatchSize,
			RemoteClients:             remoteClients,
		}
	}

	// Add the global add attributes to the log config
	addAttributes := cfg.AddAttributes
	liftAttributes := cfg.LiftAttributes

	sort.Slice(cfg.LiftResources, func(i, j int) bool {
		return cfg.LiftResources[i].Name < cfg.LiftResources[j].Name
	})

	logsMapping := schema.DefaultLogsMapping
	var sortedLiftedResources []string
	for _, v := range cfg.LiftResources {
		sortedLiftedResources = append(sortedLiftedResources, v.Name)
		if v.ColumnName != "" {
			logsMapping = logsMapping.AddStringMapping(v.ColumnName)
			continue
		}
		logsMapping = logsMapping.AddStringMapping(v.Name)
	}

	// Update the default mapping so pooled csv encoders can use the lifted columns
	schema.DefaultLogsMapping = logsMapping

	if cfg.OtelLog != nil {
		addAttributes = mergeMaps(addAttributes, cfg.OtelLog.AddAttributes)
		liftAttributes = unionSlice(liftAttributes, cfg.OtelLog.LiftAttributes)
	}

	opts := &collector.ServiceOpts{
		EnablePprof:            cfg.EnablePprof,
		Scraper:                scraperOpts,
		ListenAddr:             cfg.ListenAddr,
		NodeName:               hostname,
		Endpoint:               endpoint,
		StorageBackend:         backend,
		DisableGzip:            cfg.DisableGzip,
		LiftLabels:             sortedLiftedLabels,
		AddAttributes:          addAttributes,
		LiftAttributes:         liftAttributes,
		LiftResources:          sortedLiftedResources,
		InsecureSkipVerify:     cfg.InsecureSkipVerify,
		TLSCertFile:            cfg.TLSCertFile,
		TLSKeyFile:             cfg.TLSKeyFile,
		MaxConnections:         cfg.MaxConnections,
		MaxBatchSize:           cfg.MaxBatchSize,
		MaxSegmentAge:          time.Duration(cfg.MaxSegmentAgeSeconds) * time.Second,
		MaxSegmentSize:         cfg.MaxSegmentSize,
		MaxDiskUsage:           cfg.MaxDiskUsage,
		MaxTransferConcurrency: cfg.MaxTransferConcurrency,
		WALFlushInterval:       time.Duration(cfg.WALFlushIntervalMilliSeconds) * time.Millisecond,
		Region:                 cfg.Region,
		StorageDir:             cfg.StorageDir,
	}

	for _, v := range cfg.PrometheusRemoteWrite {
		// Add this pods identity for all metrics received
		addLabels := mergeMaps(cfg.AddLabels, map[string]string{
			"adxmon_namespace": k8s.Instance.Namespace,
			"adxmon_pod":       k8s.Instance.Pod,
			"adxmon_container": k8s.Instance.Container,
			"adxmon_database":  v.Database,
		})

		dropLabels := make(map[*regexp.Regexp]*regexp.Regexp)
		for k, v := range mergeMaps(cfg.DropLabels, v.DropLabels) {
			metricRegex, err := regexp.Compile(k)
			if err != nil {
				logger.Fatalf("invalid metric regex: %s", err)
			}

			labelRegex, err := regexp.Compile(v)
			if err != nil {
				logger.Fatalf("invalid label regex: %s", err)
			}

			dropLabels[metricRegex] = labelRegex
		}

		dropMetrics := []*regexp.Regexp{}
		for _, v := range unionSlice(cfg.DropMetrics, v.DropMetrics) {
			metricRegex, err := regexp.Compile(v)
			if err != nil {
				logger.Fatalf("invalid metric regex: %s", err)
			}

			dropMetrics = append(dropMetrics, metricRegex)
		}

		keepMetrics := []*regexp.Regexp{}
		for _, v := range unionSlice(cfg.KeepMetrics, v.KeepMetrics) {
			metricRegex, err := regexp.Compile(v)
			if err != nil {
				logger.Fatalf("invalid metric regex: %s", err)
			}

			keepMetrics = append(keepMetrics, metricRegex)
		}

		keepMetricLabelValues := make(map[*regexp.Regexp]*regexp.Regexp)
		for _, v := range append(cfg.KeepMetricsWithLabelValue, v.KeepMetricsWithLabelValue...) {
			labelRe, err := regexp.Compile(v.LabelRegex)
			if err != nil {
				logger.Fatalf("invalid metric regex: %s", err)
			}

			valueRe, err := regexp.Compile(v.ValueRegex)
			if err != nil {
				logger.Fatalf("invalid label regex: %s", err)
			}

			keepMetricLabelValues[labelRe] = valueRe
		}

		var defaultDropMetrics bool
		if cfg.DefaultDropMetrics != nil {
			defaultDropMetrics = *cfg.DefaultDropMetrics
		}

		if v.DefaultDropMetrics != nil {
			defaultDropMetrics = *v.DefaultDropMetrics
		}

		var disableMetricsForwarding bool
		if v.DisableMetricsForwarding != nil {
			disableMetricsForwarding = *v.DisableMetricsForwarding
		}

		remoteWriteClients, err := getMetricsExporters(v.Exporters, cfg.Exporters, metricExporterCache)
		if err != nil {
			return fmt.Errorf("prometheus remote write: %w", err)
		}

		opts.PromMetricsHandlers = append(opts.PromMetricsHandlers, collector.PrometheusRemoteWriteHandlerOpts{
			Path: v.Path,
			MetricOpts: collector.MetricsHandlerOpts{
				DefaultDropMetrics:       defaultDropMetrics,
				AddLabels:                addLabels,
				DropMetrics:              dropMetrics,
				DropLabels:               dropLabels,
				KeepMetrics:              keepMetrics,
				KeepMetricsLabelValues:   keepMetricLabelValues,
				DisableMetricsForwarding: disableMetricsForwarding,
				RemoteWriteClients:       remoteWriteClients,
			},
		})
	}

	for _, v := range cfg.OtelMetric {
		// Add this pods identity for all metrics received
		addLabels := mergeMaps(cfg.AddLabels, v.AddLabels, map[string]string{
			"adxmon_namespace": k8s.Instance.Namespace,
			"adxmon_pod":       k8s.Instance.Pod,
			"adxmon_container": k8s.Instance.Container,
			"adxmon_database":  v.Database,
		})

		dropLabels := make(map[*regexp.Regexp]*regexp.Regexp)
		for k, v := range mergeMaps(cfg.DropLabels, v.DropLabels) {
			metricRegex, err := regexp.Compile(k)
			if err != nil {
				logger.Fatalf("invalid metric regex: %s", err)
			}

			labelRegex, err := regexp.Compile(v)
			if err != nil {
				logger.Fatalf("invalid label regex: %s", err)
			}

			dropLabels[metricRegex] = labelRegex
		}

		dropMetrics := []*regexp.Regexp{}
		for _, v := range unionSlice(cfg.DropMetrics, v.DropMetrics) {
			metricRegex, err := regexp.Compile(v)
			if err != nil {
				logger.Fatalf("invalid metric regex: %s", err)
			}

			dropMetrics = append(dropMetrics, metricRegex)
		}

		keepMetrics := []*regexp.Regexp{}
		for _, v := range unionSlice(cfg.KeepMetrics, v.KeepMetrics) {
			metricRegex, err := regexp.Compile(v)
			if err != nil {
				logger.Fatalf("invalid metric regex: %s", err)
			}

			keepMetrics = append(keepMetrics, metricRegex)
		}

		keepMetricLabelValues := make(map[*regexp.Regexp]*regexp.Regexp)
		for _, v := range append(cfg.KeepMetricsWithLabelValue, v.KeepMetricsWithLabelValue...) {
			labelRe, err := regexp.Compile(v.LabelRegex)
			if err != nil {
				logger.Fatalf("invalid metric regex: %s", err)
			}

			valueRe, err := regexp.Compile(v.ValueRegex)
			if err != nil {
				logger.Fatalf("invalid label regex: %s", err)
			}

			keepMetricLabelValues[labelRe] = valueRe
		}

		var defaultDropMetrics bool
		if cfg.DefaultDropMetrics != nil {
			defaultDropMetrics = *cfg.DefaultDropMetrics
		}

		if v.DefaultDropMetrics != nil {
			defaultDropMetrics = *v.DefaultDropMetrics
		}

		var disableMetricsForwarding bool
		if v.DisableMetricsForwarding != nil {
			disableMetricsForwarding = *v.DisableMetricsForwarding
		}

		remoteWriteClients, err := getMetricsExporters(v.Exporters, cfg.Exporters, metricExporterCache)
		if err != nil {
			return fmt.Errorf("otel metric: %w", err)
		}

		opts.OtlpMetricsHandlers = append(opts.OtlpMetricsHandlers, collector.OtlpMetricsHandlerOpts{
			Path:     v.Path,
			GrpcPort: v.GrpcPort,
			MetricOpts: collector.MetricsHandlerOpts{
				DefaultDropMetrics:       defaultDropMetrics,
				AddLabels:                addLabels,
				DropMetrics:              dropMetrics,
				DropLabels:               dropLabels,
				KeepMetrics:              keepMetrics,
				KeepMetricsLabelValues:   keepMetricLabelValues,
				DisableMetricsForwarding: disableMetricsForwarding,
				RemoteWriteClients:       remoteWriteClients,
			},
		})
	}

	if cfg.OtelLog != nil {
		v := cfg.OtelLog
		addAttributes := mergeMaps(cfg.AddAttributes, v.AddAttributes)

		createHttpFunc := func(store storage.Store, health *cluster.Health) (*logs.Service, *http.HttpHandler, error) {
			transformers := []types.Transformer{}
			if v.Transforms != nil {
				for _, t := range v.Transforms {
					transform, err := transforms.NewTransform(t.Name, t.Config)
					if err != nil {
						return nil, nil, fmt.Errorf("create transform: %w", err)
					}
					transformers = append(transformers, transform)
				}
			}

			if len(addAttributes) > 0 {
				transformers = append(transformers, addattributes.NewTransform(addattributes.Config{
					ResourceValues: addAttributes,
				}))
			}

			sink, err := sinks.NewStoreSink(sinks.StoreSinkConfig{
				Store: store,
			})
			if err != nil {
				return nil, nil, fmt.Errorf("failed to create store sink: %w", err)
			}

			sinks := []types.Sink{sink}
			for _, exporterName := range v.Exporters {
				sink, err := config.GetLogsExporter(exporterName, cfg.Exporters)
				if err != nil {
					return nil, nil, fmt.Errorf("failed to get exporter %s: %w", exporterName, err)
				}
				sinks = append(sinks, sink)
			}

			logsSvc := otlp.NewLogsService(otlp.LogsServiceOpts{
				WorkerCreator: engine.WorkerCreator(transformers, sinks),
				HealthChecker: health,
			})

			workerSvc := &logs.Service{
				Source:     logsSvc,
				Transforms: transformers,
				Sinks:      sinks,
			}
			httpHandler := &http.HttpHandler{
				Path:    "/v1/logs",
				Handler: logsSvc.Handler,
			}
			return workerSvc, httpHandler, nil
		}

		opts.HttpLogCollectorOpts = append(opts.HttpLogCollectorOpts, collector.HttpLogCollectorOpts{
			CreateHTTPSvc: createHttpFunc,
		})
	}

	for _, v := range cfg.HostLog {
		tailSourceConfig := tail.TailSourceConfig{}
		if !v.DisableKubeDiscovery {
			informer, err = getInformer(cfg.Kubeconfig, hostname, informer)
			if err != nil {
				return fmt.Errorf("failed to get informer for tail: %w", err)
			}

			var staticPodTargets []*tail.StaticPodTargets
			for _, target := range v.StaticPodTargets {
				staticPodTargets = append(staticPodTargets, &tail.StaticPodTargets{
					Namespace:   target.Namespace,
					Name:        target.Name,
					Labels:      target.LabelTargets,
					Parsers:     target.Parsers,
					Destination: target.Destination,
				})
			}

			tailSourceConfig.PodDiscoveryOpts = &tail.PodDiscoveryOpts{
				NodeName:         hostname,
				PodInformer:      informer,
				StaticPodTargets: staticPodTargets,
			}
		}

		createFunc := func(store storage.Store) (*logs.Service, error) {
			addAttributes := mergeMaps(cfg.AddLabels, v.AddAttributes, map[string]string{
				"adxmon_namespace": k8s.Instance.Namespace,
				"adxmon_pod":       k8s.Instance.Pod,
				"adxmon_container": k8s.Instance.Container,
			})

			staticTargets := []tail.FileTailTarget{}
			for _, target := range v.StaticFileTargets {
				staticTargets = append(staticTargets, tail.FileTailTarget{
					FilePath: target.FilePath,
					LogType:  target.LogType,
					Database: target.Database,
					Table:    target.Table,
					Parsers:  target.Parsers,
				})
			}

			transformers := []types.Transformer{}
			for _, t := range v.Transforms {
				transform, err := transforms.NewTransform(t.Name, t.Config)
				if err != nil {
					return nil, fmt.Errorf("create transform: %w", err)
				}
				transformers = append(transformers, transform)
			}

			if len(addAttributes) > 0 {
				transformers = append(transformers, addattributes.NewTransform(addattributes.Config{
					ResourceValues: addAttributes,
				}))
			}

			sink, err := sinks.NewStoreSink(sinks.StoreSinkConfig{
				Store: store,
			})
			if err != nil {
				return nil, fmt.Errorf("create sink for tailsource: %w", err)
			}

			tailSourceConfig.StaticTargets = staticTargets
			tailSourceConfig.CursorDirectory = cfg.StorageDir
			sinks := []types.Sink{sink}

			for _, exporterName := range v.Exporters {
				sink, err := config.GetLogsExporter(exporterName, cfg.Exporters)
				if err != nil {
					return nil, fmt.Errorf("failed to get exporter %s: %w", exporterName, err)
				}
				sinks = append(sinks, sink)
			}

			workerCreator := engine.WorkerCreator(
				transformers,
				sinks,
			)
			tailSourceConfig.WorkerCreator = workerCreator

			source, err := tail.NewTailSource(tailSourceConfig)
			if err != nil {
				return nil, fmt.Errorf("create tailsource: %w", err)
			}

			return &logs.Service{
				Source:     source,
				Transforms: transformers,
				Sinks:      sinks,
			}, nil
		}

		opts.LogCollectionHandlers = append(opts.LogCollectionHandlers, collector.LogCollectorOpts{
			Create: createFunc,
		})

		if len(v.KernelTargets) > 0 {
			kernelCreateFunction := func(store storage.Store) (*logs.Service, error) {
				addAttributes := mergeMaps(cfg.AddLabels, v.AddAttributes)

				sink, err := sinks.NewStoreSink(sinks.StoreSinkConfig{
					Store: store,
				})
				if err != nil {
					return nil, fmt.Errorf("create sink for tailsource: %w", err)
				}

				transformers := []types.Transformer{}
				for _, t := range v.Transforms {
					transform, err := transforms.NewTransform(t.Name, t.Config)
					if err != nil {
						return nil, fmt.Errorf("create transform: %w", err)
					}
					transformers = append(transformers, transform)
				}
				attributeTransform := addattributes.NewTransform(addattributes.Config{
					ResourceValues: addAttributes,
				})
				transformers = append(transformers, attributeTransform)

				var targets []kernel.KernelTargetConfig
				for _, target := range v.KernelTargets {
					targets = append(targets, kernel.KernelTargetConfig{
						Database:       target.Database,
						Table:          target.Table,
						PriorityFilter: target.Priority,
					})
				}

				sinks := []types.Sink{sink}
				for _, exporterName := range v.Exporters {
					sink, err := config.GetLogsExporter(exporterName, cfg.Exporters)
					if err != nil {
						return nil, fmt.Errorf("failed to get exporter %s: %w", exporterName, err)
					}
					sinks = append(sinks, sink)
				}

				kernelSourceConfig := kernel.KernelSourceConfig{
					WorkerCreator:   engine.WorkerCreator(transformers, sinks),
					CursorDirectory: cfg.StorageDir,
					Targets:         targets,
				}

				source, err := kernel.NewKernelSource(kernelSourceConfig)
				if err != nil {
					return nil, fmt.Errorf("create kernel source: %w", err)
				}

				return &logs.Service{
					Source:     source,
					Transforms: transformers,
					Sinks:      sinks,
				}, nil
			}
			opts.LogCollectionHandlers = append(opts.LogCollectionHandlers, collector.LogCollectorOpts{
				Create: kernelCreateFunction,
			})
		}

		if len(v.JournalTargets) > 0 {
			journalCreateFunction := func(store storage.Store) (*logs.Service, error) {
				addAttributes := mergeMaps(cfg.AddLabels, v.AddAttributes)

				// TODO - combine these with shared file tail code
				transformers := []types.Transformer{}
				for _, t := range v.Transforms {
					transform, err := transforms.NewTransform(t.Name, t.Config)
					if err != nil {
						return nil, fmt.Errorf("create transform: %w", err)
					}
					transformers = append(transformers, transform)
				}

				if len(addAttributes) > 0 {
					transformers = append(transformers, addattributes.NewTransform(addattributes.Config{
						ResourceValues: addAttributes,
					}))
				}

				sink, err := sinks.NewStoreSink(sinks.StoreSinkConfig{
					Store: store,
				})
				if err != nil {
					return nil, fmt.Errorf("create sink for tailsource: %w", err)
				}

				targets := make([]journal.JournalTargetConfig, 0, len(v.JournalTargets))
				for _, target := range v.JournalTargets {
					parsers := parser.NewParsers(target.Parsers, "journal")

					targets = append(targets, journal.JournalTargetConfig{
						Matches:        target.Matches,
						Database:       target.Database,
						Table:          target.Table,
						LogLineParsers: parsers,
						JournalFields:  target.JournalFields,
					})
				}

				sinks := []types.Sink{sink}
				for _, exporterName := range v.Exporters {
					sink, err := config.GetLogsExporter(exporterName, cfg.Exporters)
					if err != nil {
						return nil, fmt.Errorf("failed to get exporter %s: %w", exporterName, err)
					}
					sinks = append(sinks, sink)
				}
				journalConfig := journal.SourceConfig{
					Targets:         targets,
					CursorDirectory: cfg.StorageDir,
					WorkerCreator:   engine.WorkerCreator(transformers, sinks),
				}

				source := journal.New(journalConfig)

				return &logs.Service{
					Source:     source,
					Transforms: transformers,
					Sinks:      sinks,
				}, nil
			}

			opts.LogCollectionHandlers = append(opts.LogCollectionHandlers, collector.LogCollectorOpts{
				Create: journalCreateFunction,
			})
		}
	}

	svcCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc, err := collector.NewService(opts)
	if err != nil {
		return err
	}
	if err := svc.Open(svcCtx); err != nil {
		return err
	}

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sc
		cancel()

		logger.Infof("Received signal %s, exiting...", sig.String())
		// Shutdown the server and cancel context
		err := svc.Close()
		if err != nil {
			logger.Error(err.Error())
		}
	}()
	<-svcCtx.Done()
	return nil
}

// unionSlice returns a union of two string slices
func unionSlice(a []string, b []string) []string {
	m := make(map[string]struct{})
	for _, v := range a {
		m[v] = struct{}{}
	}

	for _, v := range b {
		m[v] = struct{}{}
	}

	var result []string
	for k := range m {
		result = append(result, k)
	}

	return result
}

func mergeMaps(labels ...map[string]string) map[string]string {
	m := make(map[string]string)
	for _, l := range labels {
		for k, v := range l {
			m[k] = v
		}
	}
	return m
}

func getInformer(kubeConfig string, nodeName string, informer *k8s.PodInformer) (*k8s.PodInformer, error) {
	if informer != nil {
		return informer, nil
	}

	config, err := k8s.BuildConfigFromFlags("", kubeConfig)
	if err != nil {
		logger.Warnf("No kube-config provided")
		return nil, fmt.Errorf("unable to find kube config [%s]: %w", kubeConfig, err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("unable to build kube config: %w", err)
	}

	return k8s.NewPodInformer(client, nodeName), nil
}

func getMetricsExporters(exporterNames []string, exporters *config.Exporters, cache map[string]remote.RemoteWriteClient) ([]remote.RemoteWriteClient, error) {
	var remoteClients []remote.RemoteWriteClient
	for _, exporterName := range exporterNames {
		if client, ok := cache[exporterName]; ok {
			remoteClients = append(remoteClients, client)
			continue
		}

		remoteClient, err := config.GetMetricsExporter(exporterName, exporters)
		if err != nil {
			return nil, fmt.Errorf("failed to get exporter %s: %w", exporterName, err)
		}

		remoteClients = append(remoteClients, remoteClient)
		cache[exporterName] = remoteClient
	}
	return remoteClients, nil
}
