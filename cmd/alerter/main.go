package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Azure/adx-mon/alerter"
	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/urfave/cli/v2" // imports as package "cli"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func main() {
	app := &cli.App{
		Name:  "alerter",
		Usage: "adx-mon alerting engine for ADX",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{Name: "kusto-endpoint", Usage: "Kusto endpoint in the format of <name>=<endpoint>"},
			&cli.StringFlag{Name: "kubeconfig", Usage: "/etc/kubernetes/admin.conf"},
			&cli.IntFlag{Name: "port", Value: 4023, Usage: "Metrics port number"},
			&cli.StringFlag{Name: "auth-msi-id", Usage: "MSI client ID for authentication to Kusto"},
			&cli.StringFlag{Name: "auth-token", Usage: "Application token for authentication to Kusto"},
			&cli.StringFlag{Name: "cloud", Usage: "Azure cloud"},
			&cli.StringFlag{Name: "region", Usage: "Current region"},
			&cli.StringFlag{Name: "alerter-address", Usage: "Address of the alert notification service"},
			&cli.IntFlag{Name: "concurrency", Value: 10, Usage: "Number of concurrent queries to run"},
			&cli.IntFlag{Name: "max-notifications", Value: 25, Usage: "Maximum number of notifications to send per rule"},
			&cli.StringSliceFlag{Name: "tag", Usage: "Tag in the format of <key>=<value> that applies to execution context"},
		},
		Action: realMain,
		Commands: []*cli.Command{
			NewLintCommand(),
		},
	}

	if err := app.Run(os.Args); err != nil {
		logger.Fatalf(err.Error())
	}
}

func realMain(ctx *cli.Context) error {
	endpoints := make(map[string]string)
	endpointsArg := ctx.StringSlice("kusto-endpoint")
	for _, v := range endpointsArg {
		parts := strings.Split(v, "=")
		if len(parts) != 2 {
			return cli.Exit("Invalid kusto-endpoint format, expected <name>=<endpoint>", 1)
		}
		endpoints[parts[0]] = parts[1]
	}

	tags := make(map[string]string)
	tagsArg := ctx.StringSlice("tag")
	for _, v := range tagsArg {
		parts := strings.Split(v, "=")
		if len(parts) != 2 {
			return cli.Exit("Invalid tag format, expected <key>=<value>", 1)
		}
		tags[strings.ToLower(parts[0])] = strings.ToLower(parts[1])
	}

	// Always add region and cloud tags which are required params for alerter currently.
	tags["region"] = strings.ToLower(ctx.String("region"))
	tags["cloud"] = strings.ToLower(ctx.String("cloud"))

	for k, v := range tags {
		logger.Infof("Using tag %s=%s", k, v)
	}

	scheme := clientgoscheme.Scheme
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return err
	}
	if err := alertrulev1.AddToScheme(scheme); err != nil {
		return err
	}

	_, _, ctrlCli, err := newKubeClient(ctx)
	if err != nil {
		return err
	}

	opts := &alerter.AlerterOpts{
		Port:             ctx.Int("port"),
		KustoEndpoints:   endpoints,
		Region:           ctx.String("region"),
		Cloud:            ctx.String("cloud"),
		AlertAddr:        ctx.String("alerter-address"),
		Concurrency:      ctx.Int("concurrency"),
		MaxNotifications: ctx.Int("max-notifications"),
		MSIID:            ctx.String("auth-msi-id"),
		KustoToken:       ctx.String("auth-token"),
		Tags:             tags,
		CtrlCli:          ctrlCli,
	}

	svcCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if ctx.String("lint-dir") != "" {
		return alerter.Lint(svcCtx, opts, ctx.String("lint-dir"))
	}

	svc, err := alerter.NewService(opts)
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
