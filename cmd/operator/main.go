package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	operator "github.com/Azure/adx-mon/operator"
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
		Name:    "operator",
		Usage:   "adx-mon operator",
		Flags:   []cli.Flag{},
		Action:  realMain,
		Version: version.String(),
	}

	if err := app.Run(os.Args); err != nil {
		logger.Fatal(err.Error())
	}
}

func realMain(ctx *cli.Context) error {
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
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0", // Disable metrics server
		},
	})
	if err != nil {
		return fmt.Errorf("unable to create manager: %w", err)
	}

	// Set up controllers
	adxr := &operator.AdxReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
	if err = adxr.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create adx controller: %w", err)
	}

	ir := &operator.IngestorReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
	if err = ir.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create ingestor controller: %w", err)
	}

	ar := &operator.AlerterReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
	if err = ar.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create alerter controller: %w", err)
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
