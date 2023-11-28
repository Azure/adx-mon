package main

import (
	"context"
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
	"github.com/Azure/adx-mon/pkg/logger"
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
			&cli.StringFlag{Name: "kubeconfig", Usage: "/etc/kubernetes/kubelet.conf"},
			&cli.StringFlag{Name: "config", Usage: "Config file path"},
			&cli.BoolFlag{Name: "experimental-log-collection", Usage: "Enable experimental log collection.", Hidden: true},
		},

		Commands: []*cli.Command{
			{
				Name:  "config",
				Usage: "Generate a config file",
				Action: func(c *cli.Context) error {
					b, err := toml.Marshal(DefaultConfig)
					if err != nil {
						return err
					}
					fmt.Println(string(b))
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
	if configFile != "" {

		configBytes, err := os.ReadFile(configFile)
		if err != nil {
			return err
		}

		var fileConfig Config
		if err := toml.Unmarshal(configBytes, &fileConfig); err != nil {
			return err
		}
		cfg = fileConfig
	}

	_, k8scli, _, err := newKubeClient(ctx)
	if err != nil {
		return err
	}

	addLabels := cfg.AddLabels
	addAttributes := cfg.AddAttributes

	dropLabels := make(map[*regexp.Regexp]*regexp.Regexp)
	for k, v := range cfg.DropLabels {
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
	for _, v := range cfg.DropMetrics {
		metricRegex, err := regexp.Compile(v)
		if err != nil {
			logger.Fatalf("invalid metric regex: %s", err)
		}

		dropMetrics = append(dropMetrics, metricRegex)
	}

	hostname := cfg.Hostname
	if hostname == "" {
		var err error
		hostname, err = os.Hostname()
		if err != nil {
			return fmt.Errorf("failed to get hostname: %w", err)
		}
	}

	var staticTargets []collector.ScrapeTarget
	for _, target := range cfg.PrometheusScrape.StaticScrapeTarget {
		if match, err := regexp.MatchString(target.HostRegex, hostname); err != nil {
			return fmt.Errorf("failed to match hostname %s with regex %s: %w", hostname, target.HostRegex, err)
		} else if !match {
			continue
		}

		url := target.URL
		namespace := target.Namespace
		pod := target.Pod
		container := target.Container

		staticTargets = append(staticTargets, collector.ScrapeTarget{
			Addr:      url,
			Namespace: namespace,
			Pod:       pod,
			Container: container,
		})
	}

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
		}
	}

	if cfg.StorageDir == "" {
		logger.Fatalf("storage-dir is required")
	} else {
		logger.Infof("Using storage dir: %s", cfg.StorageDir)
	}

	opts := &collector.ServiceOpts{
		K8sCli:                   k8scli,
		ListenAddr:               cfg.ListenAddr,
		ScrapeInterval:           time.Duration(cfg.PrometheusScrape.ScrapeIntervalSeconds) * time.Second,
		NodeName:                 hostname,
		Targets:                  staticTargets,
		Endpoints:                endpoints,
		DropMetrics:              dropMetrics,
		AddLabels:                addLabels,
		AddAttributes:            addAttributes,
		LiftAttributes:           cfg.LiftAttributes,
		DropLabels:               dropLabels,
		InsecureSkipVerify:       cfg.InsecureSkipVerify,
		MaxBatchSize:             cfg.MaxBatchSize,
		CollectLogs:              ctx.Bool("experimental-log-collection"),
		DisableMetricsForwarding: cfg.DisableMetricsForwarding,
		StorageDir:               cfg.StorageDir,
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

func newKubeClient(cCtx *cli.Context) (dynamic.Interface, *kubernetes.Clientset, ctrlclient.Client, error) {
	config, err := clientcmd.BuildConfigFromFlags("", cCtx.String("kubeconfig"))
	if err != nil {
		logger.Warnf("No kube config provided, using fake kube client")
		return nil, nil, nil, fmt.Errorf("unable to find kube config [%s]: %v", cCtx.String("kubeconfig"), err)
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
