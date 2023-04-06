package main

import (
	"context"
	"fmt"
	"github.com/Azure/adx-mon/collector"
	"github.com/Azure/adx-mon/logger"
	"github.com/urfave/cli/v2"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
	"syscall"
	"time"
)

func main() {
	app := &cli.App{
		Name:  "collector",
		Usage: "adx-mon metrics collector",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "kubeconfig", Usage: "/etc/kubernetes/kubelet.conf"},
			&cli.StringFlag{Name: "hostname", Usage: "Hostname filter override"},
			&cli.StringSliceFlag{Name: "add-labels", Usage: "Label in the format of <name>=<value>.  These are added to all metrics collected by this agent"},
			&cli.StringSliceFlag{Name: "drop-labels", Usage: "Labels to drop if they exist.  These are dropped from all metrics collected by this agent"},
			&cli.StringSliceFlag{Name: "target", Usage: "Static Prometheus scrape target in the format of <host regex>=<url>.  Host names that match the regex will scrape the target url"},
			&cli.StringSliceFlag{Name: "endpoints", Usage: "Prometheus remote write endpoint URLs"},
			&cli.StringFlag{Name: "listen-addr", Usage: "Address to listen on for Prometheus scrape requests", Value: ":8080"},
			&cli.DurationFlag{Name: "scrape-interval", Usage: "Scrape interval", Value: 30 * time.Second},
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

	hostname := ctx.String("hostname")
	if hostname == "" {
		var err error
		hostname, err = os.Hostname()
		if err != nil {
			return fmt.Errorf("failed to get hostname: %w", err)
		}
	}

	var staticTargets []string
	for _, target := range ctx.StringSlice("target") {
		split := strings.Split(target, "=")
		if len(split) != 2 {
			return fmt.Errorf("invalid target %s", target)
		}

		if match, err := regexp.MatchString(split[0], hostname); err != nil {
			return fmt.Errorf("failed to match hostname %s with regex %s: %w", hostname, split[0], err)
		} else if !match {
			continue
		}

		staticTargets = append(staticTargets, split[1])
	}

	opts := &collector.ServiceOpts{
		K8sCli:         k8scli,
		ListentAddr:    ctx.String("listen-addr"),
		ScrapeInterval: ctx.Duration("scrape-interval"),
		NodeName:       hostname,
		Targets:        staticTargets,
		Endpoints:      ctx.StringSlice("endpoints"),
		AddLabels:      addLabels,
		DropLabels:     dropLabels,
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
