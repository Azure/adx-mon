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
	"strings"
	"syscall"
	"time"

	"github.com/Azure/adx-mon/collector"
	"github.com/Azure/adx-mon/pkg/k8s"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/pelletier/go-toml/v2"
	"github.com/urfave/cli/v2"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	// Trigger initial identity load
	_ "github.com/Azure/adx-mon/pkg/k8s"
)

func main() {
	app := &cli.App{
		Name:      "collector",
		Usage:     "adx-mon metrics collector",
		UsageText: ``,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "hostname", Usage: "Hostname filter override"},
			&cli.StringFlag{Name: "config", Usage: "Config file path"},
			&cli.BoolFlag{Name: "experimental-log-collection", Usage: "Enable experimental log collection.", Hidden: true},
		},

		Commands: []*cli.Command{
			{
				Name:  "config",
				Usage: "Generate a config file",
				Action: func(c *cli.Context) error {
					buf := bytes.Buffer{}
					enc := toml.NewEncoder(&buf)
					enc.SetIndentTables(true)
					if err := enc.Encode(DefaultConfig); err != nil {
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
	}

	if err := app.Run(os.Args); err != nil {
		logger.Fatalf(err.Error())
	}
}

func realMain(ctx *cli.Context) error {
	runtime.MemProfileRate = 4096
	runtime.SetBlockProfileRate(int(1 * time.Second))
	runtime.SetMutexProfileFraction(1)

	var cfg = DefaultConfig
	configFile := ctx.String("config")

	if configFile == "" {
		logger.Fatalf("config file is required.  Run `collector config` to generate a config file")
	}

	configBytes, err := os.ReadFile(configFile)
	if err != nil {
		return err
	}

	var fileConfig Config
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

	if err := cfg.Validate(); err != nil {
		return err
	}

	hostname := ctx.String("hostname")
	if hostname == "" {
		var err error
		hostname, err = os.Hostname()
		if err != nil {
			return fmt.Errorf("failed to get hostname: %w", err)
		}
	}

	cfg.ReplaceVariable("$(HOSTNAME)", hostname)

	// Set some lower level WAL values to use less memory from the defaults which are set for ingestor.
	wal.SetEncoderPoolSize(1)
	wal.SetDecoderPoolSize(1)

	var endpoints []string
	if cfg.Endpoint != "" {
		for _, endpoint := range []string{cfg.Endpoint} {
			u, err := url.Parse(endpoint)
			if err != nil {
				return fmt.Errorf("failed to parse endpoint %s: %w", endpoint, err)
			}

			if u.Scheme != "http" && u.Scheme != "https" {
				return fmt.Errorf("endpoint %s must be http or https", endpoint)
			}

			logger.Infof("Using remote write endpoint %s", endpoint)
			endpoints = append(endpoints, endpoint)
		}
	}

	if cfg.StorageDir == "" {
		logger.Fatalf("storage-dir is required")
	} else {
		logger.Infof("Using storage dir: %s", cfg.StorageDir)
	}

	var scraperOpts *collector.ScraperOpts
	if cfg.PrometheusScrape != nil {
		_, k8scli, _, err := newKubeClient(cfg.PrometheusScrape.Kubeconfig)
		if err != nil {
			return err
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

		scraperOpts = &collector.ScraperOpts{
			NodeName:                  hostname,
			K8sCli:                    k8scli,
			Database:                  cfg.PrometheusScrape.Database,
			AddLabels:                 addLabels,
			DropLabels:                dropLabels,
			DropMetrics:               dropMetrics,
			KeepMetrics:               keepMetrics,
			KeepMetricsWithLabelValue: keepMetricLabelValues,
			DefaultDropMetrics:        defaultDropMetrics,
			DisableMetricsForwarding:  cfg.PrometheusScrape.DisableMetricsForwarding,
			ScrapeInterval:            time.Duration(cfg.PrometheusScrape.ScrapeIntervalSeconds) * time.Second,
			Targets:                   staticTargets,
			MaxBatchSize:              cfg.MaxBatchSize,
		}
	}

	// Add the global add attributes to the log config
	addAttributes := cfg.AddAttributes
	liftAttributes := cfg.LiftAttributes

	if cfg.OtelLog != nil {
		addAttributes = mergeMaps(addAttributes, cfg.OtelLog.AddAttributes)
		liftAttributes = unionSlice(liftAttributes, cfg.OtelLog.LiftAttributes)
	}

	opts := &collector.ServiceOpts{
		EnablePprof:        cfg.EnablePprof,
		Scraper:            scraperOpts,
		ListenAddr:         cfg.ListenAddr,
		NodeName:           hostname,
		Endpoints:          endpoints,
		AddAttributes:      addAttributes,
		LiftAttributes:     liftAttributes,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
		MaxBatchSize:       cfg.MaxBatchSize,
		MaxSegmentAge:      time.Duration(cfg.MaxSegmentAgeSeconds) * time.Second,
		MaxSegmentSize:     cfg.MaxSegmentSize,
		CollectLogs:        ctx.Bool("experimental-log-collection"),
		StorageDir:         cfg.StorageDir,
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
		for _, v := range unionSlice(cfg.KeepMetrics, cfg.PrometheusScrape.KeepMetrics) {
			metricRegex, err := regexp.Compile(v)
			if err != nil {
				logger.Fatalf("invalid metric regex: %s", err)
			}

			keepMetrics = append(keepMetrics, metricRegex)
		}

		keepMetricLabelValues := make(map[*regexp.Regexp]*regexp.Regexp)
		for _, v := range append(cfg.KeepMetricsWithLabelValue, cfg.PrometheusScrape.KeepMetricsWithLabelValue...) {
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

		opts.MetricsHandlers = append(opts.MetricsHandlers, collector.MetricsHandlerOpts{
			Path:                   v.Path,
			DefaultDropMetrics:     defaultDropMetrics,
			AddLabels:              addLabels,
			DropMetrics:            dropMetrics,
			DropLabels:             dropLabels,
			KeepMetrics:            keepMetrics,
			KeepMetricsLabelValues: keepMetricLabelValues,
		})
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
			logger.Errorf(err.Error())
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

func newKubeClient(kubeconfig string) (dynamic.Interface, *kubernetes.Clientset, ctrlclient.Client, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		logger.Warnf("No kube config provided, using fake kube client")
		return nil, nil, nil, fmt.Errorf("unable to find kube config [%s]: %v", kubeconfig, err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to build kube config: %v", err)
	}

	dyCli, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to build dynamic client: %v", err)
	}

	ctrlCli, err := ctrlclient.New(config, ctrlclient.Options{})
	if err != nil {
		return nil, nil, nil, err
	}

	return dyCli, client, ctrlCli, nil
}

// parseKeyPairs parses a list of key pairs in the form of key=value,key=value
// and returns them as a map.
func parseKeyPairs(kp []string) (map[string]string, error) {
	m := make(map[string]string)
	for _, encoded := range kp {
		split := strings.Split(encoded, "=")
		if len(split) != 2 {
			return nil, fmt.Errorf("invalid key-pair %s", encoded)
		}
		m[split[0]] = split[1]
	}
	return m, nil
}
