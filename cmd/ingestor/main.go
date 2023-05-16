package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	promingest "github.com/Azure/adx-mon/ingestor"
	"github.com/Azure/adx-mon/ingestor/adx"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/tls"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/ingest"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/urfave/cli/v2"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
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
			&cli.StringFlag{Name: "namespace", Usage: "Namespace for peer discovery"},
			&cli.StringFlag{Name: "hostname", Usage: "Hostname of the current node"},
			&cli.StringFlag{Name: "storage-dir", Usage: "Direcotry to store WAL segments"},
			&cli.StringFlag{Name: "kusto-endpoint", Usage: "Kusto endpoint in the format of <db>=<endpoint>"},
			&cli.IntFlag{Name: "uploads", Usage: "Number of concurrent uploads", Value: adx.ConcurrentUploads},
			&cli.Int64Flag{Name: "max-segment-size", Usage: "Maximum segment size in bytes", Value: 1024 * 1024 * 1024},
			&cli.DurationFlag{Name: "max-segment-age", Usage: "Maximum segment age", Value: 5 * time.Minute},
			&cli.StringSliceFlag{Name: "labels", Usage: "Static labels in the format of <name>=<value> applied to all metrics"},
			&cli.StringFlag{Name: "ca-cert", Usage: "CA certificate file"},
			&cli.StringFlag{Name: "key", Usage: "Server key file"},
			&cli.BoolFlag{Name: "insecure-skip-verify", Usage: "Skip TLS verification"},
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
		cacert, key                         string
		insecureSkipVerify                  bool
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
	cacert = ctx.String("ca-cert")
	key = ctx.String("key")
	insecureSkipVerify = ctx.Bool("insecure-skip-verify")
	namespace := ctx.String("namespace")
	hostname := ctx.String("hostname")

	if namespace == "" {
		nsBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
		if err == nil {
			namespace = strings.TrimSpace(string(nsBytes))
		}
	}

	if hostname == "" {
		hostname, err = os.Hostname()
		if err != nil {
			logger.Error("Failed to get hostname: %s", err)
		}
	}

	if cacert != "" || key != "" {
		if cacert == "" || key == "" {
			logger.Fatal("Both --ca-cert and --key are required")
		}
	} else {
		logger.Warn("Using fake TLS credentials (not for production use!)")
		certBytes, keyBytes, err := tls.NewFakeTLSCredentials()
		if err != nil {
			logger.Fatal("Failed to create fake TLS credentials: %s", err)
		}

		certFile, err := os.CreateTemp("", "cert")
		if err != nil {
			logger.Fatal("Failed to create cert temp file: %s", err)
		}

		if _, err := certFile.Write(certBytes); err != nil {
			logger.Fatal("Failed to write cert temp file: %s", err)
		}

		keyFile, err := os.CreateTemp("", "key")
		if err != nil {
			logger.Fatal("Failed to create key temp file: %s", err)
		}

		if _, err := keyFile.Write(keyBytes); err != nil {
			logger.Fatal("Failed to write key temp file: %s", err)
		}

		cacert = certFile.Name()
		key = keyFile.Name()
		insecureSkipVerify = true

		if err := certFile.Close(); err != nil {
			logger.Fatal("Failed to close cert temp file: %s", err)
		}

		if err := keyFile.Close(); err != nil {
			logger.Fatal("Failed to close key temp file: %s", err)
		}

		defer func() {
			os.Remove(certFile.Name())
			os.Remove(keyFile.Name())
		}()
	}

	logger.Info("Using TLS ca-cert=%s key=%s", cacert, key)
	if storageDir == "" {
		logger.Fatal("--storage-dir is required")
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
		K8sCli:             k8scli,
		Namespace:          namespace,
		Hostname:           hostname,
		StorageDir:         storageDir,
		Uploader:           uploader,
		MaxSegmentSize:     maxSegmentSize,
		MaxSegmentAge:      maxSegmentAge,
		InsecureSkipVerify: insecureSkipVerify,
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

	mux.HandleFunc("/debug/pprof/", pprof.Index)

	srv := &http.Server{Addr: ":9090", Handler: mux}
	srv.ErrorLog = newLooger()

	go func() {
		if err := srv.ListenAndServeTLS(cacert, key); err != nil {
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

func newLooger() *log.Logger {
	return log.New(&writerAdapter{os.Stderr}, "", log.LstdFlags)
}

type writerAdapter struct {
	io.Writer
}

var (
	tlsHandshakeError = []byte("http: TLS handshake error")
	ioEOFError        = []byte("EOF")
)

func (a *writerAdapter) Write(data []byte) (int, error) {
	// Ignore TLS handshake errors that results in just a closed connection.  Load balancer
	// health checks will cause this error to be logged.
	if bytes.Contains(data, tlsHandshakeError) && bytes.Contains(data, ioEOFError) {
		return len(data), nil
	}
	return a.Writer.Write(data)
}
