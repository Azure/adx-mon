package main

import (
	"context"
	"fmt"
	"github.com/Azure/adx-mon/adx"
	promingest "github.com/Azure/adx-mon/ingestor"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/adx-mon/storage"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/ingest"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/urfave/cli/v2"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
	"syscall"
	"time"
)

type strSliceFag []string

func (i *strSliceFag) String() string {
	return strings.Join(*i, ",")
}

func (i *strSliceFag) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func main() {
	app := &cli.App{
		Name:  "ingestor",
		Usage: "adx-mon metrics ingestor",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "kubeconfig", Usage: "/etc/kubernetes/admin.conf"},
			&cli.StringFlag{Name: "storage-dir", Usage: "Direcotry to store WAL segments"},
			&cli.StringFlag{Name: "kusto-endpoint", Usage: "Kusto endpoint in the format of <db>=<endpoint>"},
			&cli.IntFlag{Name: "uploads", Usage: "Number of concurrent uploads", Value: adx.ConcurrentUploads},
			&cli.Int64Flag{Name: "max-segment-size", Usage: "Maximum segment size in bytes", Value: 1024 * 1024 * 1024},
			&cli.DurationFlag{Name: "max-segment-age", Usage: "Maximum segment age", Value: 5 * time.Minute},
			&cli.BoolFlag{Name: "use-cli-auth", Usage: "Use CLI authentication"},
			&cli.StringSliceFlag{Name: "labels", Usage: "Static labels in the format of <name>=<value> applied to all metrics"},
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
	svcCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, k8scli, _, err := newKubeClient(ctx)
	if err != nil {
		return err
	}

	runtime.MemProfileRate = 4096
	runtime.SetBlockProfileRate(int(1 * time.Second))
	runtime.SetMutexProfileFraction(1)

	var (
		storageDir, kustoEndpoint, database string
		concurrentUploads                   int
		maxSegmentSize                      int64
		maxSegmentAge                       time.Duration
		staticColumns                       strSliceFag
	)
	storageDir = ctx.String("storage-dir")
	kustoEndpoint = ctx.String("kusto-endpoint")

	concurrentUploads = ctx.Int("uploads")
	maxSegmentSize = ctx.Int64("max-segment-size")
	maxSegmentAge = ctx.Duration("max-segment-age")
	staticColumns = ctx.StringSlice("labels")

	if storageDir == "" {
		logger.Fatal("-storage-dir is required")
	}

	for _, v := range staticColumns {
		fields := strings.Split(v, "=")
		if len(fields) != 2 {
			logger.Fatal("invalid dimension: %s", v)
		}

		storage.AddDefaultMapping(fields[0], fields[1])
	}

	uploader, err := newUploader(kustoEndpoint, database, storageDir, concurrentUploads)
	if err != nil {
		logger.Fatal("Failed to create uploader: %s", err)
	}
	defer uploader.Close()

	svc, err := promingest.NewService(promingest.ServiceOpts{
		K8sCli:         k8scli,
		StorageDir:     storageDir,
		Uploader:       uploader,
		MaxSegmentSize: maxSegmentSize,
		MaxSegmentAge:  maxSegmentAge,
	})
	if err != nil {
		logger.Fatal("Failed to create service: %s", err)
	}
	if err := svc.Open(svcCtx); err != nil {
		logger.Fatal("Failed to start service: %s", err)
	}

	logger.Info("Listening at %s", ":9090")
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/transfer", svc.HandleTransfer)
	mux.HandleFunc("/receive", svc.HandleReceive)

	srv := &http.Server{Addr: ":9090", Handler: mux}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(err.Error())
		}
	}()

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sc
		cancel()

		logger.Info("Received signal %s, exiting...", sig.String())

		srv.Shutdown(context.Background())

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

func newKustoClient(endpoint string) (ingest.QueryClient, error) {
	kcsb := kusto.NewConnectionStringBuilder(endpoint)
	kcsb.WithDefaultAzureCredential()

	return kusto.New(kcsb)
}

func newUploader(kustoEndpoint, database, storageDir string, concurrentUploads int) (adx.Uploader, error) {
	if kustoEndpoint == "" && database == "" {
		logger.Warn("No kusto endpoint provided, using fake uploader")
		return adx.NewFakeUploader(), nil
	}

	if kustoEndpoint == "" {
		return nil, fmt.Errorf("-kusto-endpoint is required")
	}

	if !strings.Contains(kustoEndpoint, "=") {
		return nil, fmt.Errorf("invalid kusto endpoint: %s", kustoEndpoint)
	}

	split := strings.Split(kustoEndpoint, "=")
	database = split[0]
	kustoEndpoint = split[1]

	if database == "" {
		return nil, fmt.Errorf("-db is required")
	}

	client, err := newKustoClient(kustoEndpoint)
	defer client.Close()

	uploader := adx.NewUploader(client, adx.UploaderOpts{
		StorageDir:        storageDir,
		Database:          database,
		ConcurrentUploads: concurrentUploads,
	})
	return uploader, err
}
