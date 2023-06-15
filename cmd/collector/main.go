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
	"github.com/urfave/cli/v2"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func main() {
	app := &cli.App{
		Name:  "collector",
		Usage: "adx-mon metrics collector",
		UsageText: `
Static Targets:

Static targets can be specified with the --target flag.  The format is <host regex>=<url>,namespace/pod/container.
This is intended to support non-kubernetes workloads.  The host regex is matched against the hostname of the node
to determine if the target will be scraped.  To scrape all nodes, use .* as the host regex.  The namespace/pod/container
is used to label metrics with a namespace, pod and container name.  This value must have two slashes.  
Multiple targets can be specified by repeating the --target flag.

Scrape port 9100 on all nodes:
  --target=.*=http://$(HOSTNAME):9100/metrics

Add a static pod scrape for etcd pods running outside of Kubernetes on masters and label metrics in kube-system namespace, etcd pod and etcd container.:
  --target=.+-master-.+=http://$(HOSTNAME):2381/metrics:kube-system/etcd/etcd
`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "kubeconfig", Usage: "/etc/kubernetes/kubelet.conf"},
			&cli.StringFlag{Name: "hostname", Usage: "Hostname filter override"},
			&cli.StringSliceFlag{Name: "target", Usage: "Static Prometheus scrape target in the format of " +
				"<host regex>=<url>:namespace/pod/container.  Multiple targets can be specified by repeating this flag. See usage for more details."},
			&cli.StringSliceFlag{Name: "endpoints", Usage: "Prometheus remote write endpoint URLs"},
			&cli.BoolFlag{Name: "insecure-skip-verify", Usage: "Skip TLS verification of remote write endpoints"},
			&cli.StringFlag{Name: "listen-addr", Usage: "Address to listen on for Prometheus scrape requests", Value: ":8080"},
			&cli.DurationFlag{Name: "scrape-interval", Usage: "Scrape interval", Value: 30 * time.Second},
			&cli.StringSliceFlag{Name: "add-labels", Usage: "Label in the format of <name>=<value>.  These are added to all metrics collected by this agent"},
			&cli.StringSliceFlag{Name: "drop-labels", Usage: "Labels to drop if they exist.  These are dropped from all metrics collected by this agent"},
			&cli.StringSliceFlag{Name: "drop-metrics", Usage: "Metrics to drop if they are scraped from a target.  All metrics with matching prefixes are dropped"},
		},

		Action: func(ctx *cli.Context) error {
			return realMain(ctx)
		},
	}

	if err := app.Run(os.Args); err != nil {
		logger.Fatal(err.Error())
	}
}

func realMain(ctx *cli.Context) error {
	runtime.MemProfileRate = 4096
	runtime.SetBlockProfileRate(int(1 * time.Second))
	runtime.SetMutexProfileFraction(1)

	_, k8scli, _, err := newKubeClient(ctx)
	if err != nil {
		return err
	}

	addLabels := make(map[string]string)
	for _, tag := range ctx.StringSlice("add-labels") {
		split := strings.Split(tag, "=")
		if len(split) != 2 {
			return fmt.Errorf("invalid tag %s", tag)
		}
		addLabels[split[0]] = split[1]
	}

	dropLabels := make(map[string]struct{})
	for _, tag := range ctx.StringSlice("drop-labels") {
		dropLabels[tag] = struct{}{}
	}

	dropMetrics := make(map[string]struct{})
	for _, tag := range ctx.StringSlice("drop-metrics") {
		dropMetrics[tag] = struct{}{}
	}

	hostname := ctx.String("hostname")
	if hostname == "" {
		var err error
		hostname, err = os.Hostname()
		if err != nil {
			return fmt.Errorf("failed to get hostname: %w", err)
		}
	}

	var staticTargets []collector.ScrapeTarget
	for _, target := range ctx.StringSlice("target") {
		split := strings.Split(target, "=")
		if len(split) != 2 {
			return fmt.Errorf("invalid target %s, Expected <host regex>=<url>:namespace/pod/container", target)
		}

		if match, err := regexp.MatchString(split[0], hostname); err != nil {
			return fmt.Errorf("failed to match hostname %s with regex %s: %w", hostname, split[0], err)
		} else if !match {
			continue
		}

		i := strings.LastIndex(split[1], ":")
		if i == -1 {
			return fmt.Errorf("invalid target %s. Missing :namespace/pod/container", target)
		}

		url := split[1][:i]
		metaPart := split[1][i+1:]

		meta := strings.Split(metaPart, "/")
		if len(meta) != 3 {
			return fmt.Errorf("invalid target %s. Expected namespace/pod/container", target)
		}
		namespace := meta[0]
		pod := meta[1]
		container := meta[2]

		staticTargets = append(staticTargets, collector.ScrapeTarget{
			Addr:      url,
			Namespace: namespace,
			Pod:       pod,
			Container: container,
		})
	}

	endpoints := ctx.StringSlice("endpoints")
	for _, endpoint := range endpoints {
		u, err := url.Parse(endpoint)
		if err != nil {
			return fmt.Errorf("failed to parse endpoint %s: %w", endpoint, err)
		}

		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("endpoint %s must be http or https", endpoint)
		}

		logger.Info("Using remote write endpoint %s", endpoint)
	}

	opts := &collector.ServiceOpts{
		K8sCli:             k8scli,
		ListentAddr:        ctx.String("listen-addr"),
		ScrapeInterval:     ctx.Duration("scrape-interval"),
		NodeName:           hostname,
		Targets:            staticTargets,
		Endpoints:          endpoints,
		DropMetrics:        dropMetrics,
		AddLabels:          addLabels,
		DropLabels:         dropLabels,
		InsecureSkipVerify: ctx.Bool("insecure-skip-verify"),
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

		logger.Info("Received signal %s, exiting...", sig.String())
		// Shutdown the server and cancel context
		err := svc.Close()
		if err != nil {
			logger.Error(err.Error())
		}
	}()
	<-svcCtx.Done()
	return nil
}

func newKubeClient(cCtx *cli.Context) (dynamic.Interface, *kubernetes.Clientset, ctrlclient.Client, error) {
	config, err := clientcmd.BuildConfigFromFlags("", cCtx.String("kubeconfig"))
	if err != nil {
		logger.Warn("No kube config provided, using fake kube client")
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
