package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	operator "github.com/Azure/adx-mon/operator"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/version"
	"github.com/urfave/cli/v2"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func main() {
	app := &cli.App{
		Name:  "operator",
		Usage: "adx-mon operator",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{Name: "cluster-labels", Usage: "Labels used to identify and distinguish operator clusters. Format: <key>=<value>"},
		},
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

	clusterLabels, err := parseClusterLabels(ctx)
	if err != nil {
		return fmt.Errorf("failed to parse cluster labels: %w", err)
	}
	operator.SetClusterLabels(clusterLabels)

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

	// Discover imagePullSecrets from the operator's own pod to propagate to created workloads.
	// This requires POD_NAME and POD_NAMESPACE environment variables to be set (via downward API).
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("unable to create kubernetes clientset: %w", err)
	}
	imagePullSecrets := operator.DiscoverImagePullSecrets(svcCtx, clientset)

	// Discover nodeSelector from the operator's own pod to propagate to created workloads.
	// This ensures created pods land on the same node pool as the operator, which typically
	// has the required managed identities and network access configured.
	nodeSelector := operator.DiscoverNodeSelector(svcCtx, clientset)

	// Discover tolerations from the operator's own pod to propagate to created workloads.
	// This ensures created pods can schedule onto the same tainted node pools as the operator.
	tolerations := operator.DiscoverTolerations(svcCtx, clientset)

	// Set up controllers
	adxr := &operator.AdxReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
	if err = adxr.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create adx controller: %w", err)
	}

	ir := &operator.IngestorReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		ImagePullSecrets: imagePullSecrets,
		NodeSelector:     nodeSelector,
		Tolerations:      tolerations,
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

	cr := &operator.CollectorReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
	if err = cr.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create collector controller: %w", err)
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

func parseClusterLabels(ctx *cli.Context) (map[string]string, error) {
	clusterLabels := make(map[string]string)
	for _, label := range ctx.StringSlice("cluster-labels") {
		split := strings.SplitN(label, "=", 2)
		if len(split) != 2 {
			return nil, fmt.Errorf("invalid cluster label format %q, expected <key>=<value>", label)
		}
		key := strings.TrimSpace(split[0])
		value := strings.TrimSpace(split[1])
		if key == "" {
			return nil, fmt.Errorf("cluster label key cannot be empty (input %q)", label)
		}
		clusterLabels[key] = value
	}
	return clusterLabels, nil
}
