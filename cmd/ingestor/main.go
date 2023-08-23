package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/logs/v1/logsv1connect"
	promingest "github.com/Azure/adx-mon/ingestor"
	"github.com/Azure/adx-mon/ingestor/adx"
	"github.com/Azure/adx-mon/ingestor/otlp"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/tls"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/ingest"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/urfave/cli/v2"
	"golang.org/x/net/netutil"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type strSliceFlag []string

func (i *strSliceFlag) String() string {
	return strings.Join(*i, ",")
}

func (i *strSliceFlag) Set(value string) error {
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
			&cli.StringFlag{Name: "storage-dir", Usage: "Directory to store WAL segments"},
			&cli.StringFlag{Name: "kusto-endpoint", Usage: "Kusto endpoint in the format of <db>=<endpoint>"},
			&cli.BoolFlag{Name: "disable-peer-discovery", Usage: "Disable peer discovery and segment transfers"},
			&cli.IntFlag{Name: "uploads", Usage: "Number of concurrent uploads", Value: adx.ConcurrentUploads},
			&cli.UintFlag{Name: "max-connections", Usage: "Max number of concurrent connection allowed.  0 for no limit", Value: 1000},
			&cli.Int64Flag{Name: "max-segment-size", Usage: "Maximum segment size in bytes", Value: 1024 * 1024 * 1024},
			&cli.Int64Flag{Name: "min-transfer-size", Usage: "Minimum segment size in bytes required for direct kusto upload", Value: 100 * 1024 * 1024},
			&cli.DurationFlag{Name: "max-segment-age", Usage: "Maximum segment age", Value: 5 * time.Minute},
			&cli.StringSliceFlag{Name: "add-labels", Usage: "Static labels in the format of <name>=<value> applied to all metrics"},
			&cli.StringSliceFlag{Name: "drop-labels", Usage: "Labels to drop if they match a metrics regex in the format <metrics regex=<label name>.  These are dropped from all metrics collected by this agent"},
			&cli.StringSliceFlag{Name: "drop-metrics", Usage: "Metrics to drop if they match the regex."},

			&cli.StringFlag{Name: "ca-cert", Usage: "CA certificate file"},
			&cli.StringFlag{Name: "key", Usage: "Server key file"},
			&cli.BoolFlag{Name: "insecure-skip-verify", Usage: "Skip TLS verification"},
			&cli.StringSliceFlag{Name: "lift-label", Usage: "Labels to lift from the metric to columns. Format is <label>[=<column name>]"},
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
		storageDir, kustoEndpoint, database      string
		cacert, key                              string
		insecureSkipVerify, disablePeerDiscovery bool
		concurrentUploads                        int
		maxConns                                 int
		maxSegmentSize, minTransferSize          int64
		maxSegmentAge                            time.Duration
	)
	storageDir = ctx.String("storage-dir")
	kustoEndpoint = ctx.String("kusto-endpoint")

	concurrentUploads = ctx.Int("uploads")
	maxSegmentSize = ctx.Int64("max-segment-size")
	maxSegmentAge = ctx.Duration("max-segment-age")
	minTransferSize = ctx.Int64("min-transfer-size")
	maxConns = int(ctx.Uint("max-connections"))
	cacert = ctx.String("ca-cert")
	key = ctx.String("key")
	insecureSkipVerify = ctx.Bool("insecure-skip-verify")
	namespace := ctx.String("namespace")
	hostname := ctx.String("hostname")
	disablePeerDiscovery = ctx.Bool("disable-peer-discovery")

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

	defaultMapping := storage.NewMetricsSchema()
	for _, v := range ctx.StringSlice("add-labels") {
		fields := strings.Split(v, "=")
		if len(fields) != 2 {
			logger.Fatal("invalid dimension: %s", v)
		}

		defaultMapping = defaultMapping.AddConstMapping(fields[0], fields[1])
	}

	liftedLabels := ctx.StringSlice("lift-label")
	sort.Strings(liftedLabels)

	var sortedLiftedLabels []string
	for _, v := range liftedLabels {
		// The format is <label>[=<column name>] where the column name is optional.  If not specified, the label name is used.
		fields := strings.Split(v, "=")
		if len(fields) > 2 {
			logger.Fatal("invalid dimension: %s", v)
		}

		sortedLiftedLabels = append(sortedLiftedLabels, fields[0])

		if len(fields) == 2 {
			defaultMapping = defaultMapping.AddStringMapping(fields[1])
			continue
		}

		defaultMapping = defaultMapping.AddStringMapping(v)
	}

	dropLabels := make(map[*regexp.Regexp]*regexp.Regexp)
	for _, v := range ctx.StringSlice("drop-labels") {
		// The format is <metrics region>=<label regex>
		fields := strings.Split(v, "=")
		if len(fields) > 2 {
			logger.Fatal("invalid dimension: %s", v)
		}

		metricRegex, err := regexp.Compile(fields[0])
		if err != nil {
			logger.Fatal("invalid metric regex: %s", err)
		}

		labelRegex, err := regexp.Compile(fields[1])
		if err != nil {
			logger.Fatal("invalid label regex: %s", err)
		}

		dropLabels[metricRegex] = labelRegex
	}

	dropMetrics := []*regexp.Regexp{}
	for _, v := range ctx.StringSlice("drop-metrics") {
		metricRegex, err := regexp.Compile(v)
		if err != nil {
			logger.Fatal("invalid metric regex: %s", err)
		}

		dropMetrics = append(dropMetrics, metricRegex)
	}

	uploader, err := newUploader(kustoEndpoint, database, storageDir, concurrentUploads, defaultMapping)
	if err != nil {
		logger.Fatal("Failed to create uploader: %s", err)
	}
	defer uploader.Close()

	svc, err := promingest.NewService(promingest.ServiceOpts{
		K8sCli:               k8scli,
		Namespace:            namespace,
		Hostname:             hostname,
		StorageDir:           storageDir,
		Uploader:             uploader,
		DisablePeerDiscovery: disablePeerDiscovery,
		MaxSegmentSize:       maxSegmentSize,
		MaxSegmentAge:        maxSegmentAge,
		MinTransferSize:      minTransferSize,
		InsecureSkipVerify:   insecureSkipVerify,
		LiftedColumns:        sortedLiftedLabels,
		DropLabels:           dropLabels,
		DropMetrics:          dropMetrics,
	})
	if err != nil {
		logger.Fatal("Failed to create service: %s", err)
	}
	if err := svc.Open(svcCtx); err != nil {
		logger.Fatal("Failed to start service: %s", err)
	}

	l, err := net.Listen("tcp", ":9090")
	if maxConns > 0 {
		logger.Info("Limiting connections to %d", maxConns)
		l = netutil.LimitListener(l, maxConns)
	}
	defer l.Close()

	logger.Info("Listening at %s", ":9090")
	mux := http.NewServeMux()
	mux.HandleFunc("/transfer", svc.HandleTransfer)
	mux.HandleFunc("/receive", svc.HandleReceive)
	mux.Handle(logsv1connect.NewLogsServiceHandler(otlp.NewLogsServer()))

	logger.Info("Metrics Listening at %s", ":9091")
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())
	metricsMux.HandleFunc("/debug/pprof/", pprof.Index)
	metricsMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	metricsMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	metricsMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	metricsMux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	srv := &http.Server{
		Handler: mux,
		// Close idle connections fairly often to establish new connections through the load balancer
		// so that long-lived connections don't stay pinned to the same node indefinitely.
		IdleTimeout: 15 * time.Second,
	}
	srv.ErrorLog = newLogger()

	go func() {
		if err := srv.ServeTLS(l, cacert, key); err != nil {
			logger.Error(err.Error())
		}
	}()

	metricsSrv := &http.Server{Addr: ":9091", Handler: metricsMux}
	metricsSrv.ErrorLog = newLogger()
	go func() {
		if err := metricsSrv.ListenAndServe(); err != nil {
			logger.Error(err.Error())
		}
	}()

	// Capture SIGINT and SIGTERM to trigger a shutdown and upload/transfer of all pending segments.
	// This is best-effort, if the process is killed with SIGKILL or the shutdown takes too long
	// the segments will be delayed until the process is restarted.
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sc

		// Stop receiving new samples
		srv.Shutdown(context.Background())

		// Disable writes for internal processes (metrics)
		if err := svc.DisableWrites(); err != nil {
			logger.Error("Failed to disable writes: %s", err)
		}

		logger.Info("Received signal %s, uploading pending segments...", sig.String())

		// Upload pending segments
		if err := svc.UploadSegments(); err != nil {
			logger.Error("Failed to upload segments: %s", err)
		}

		// Trigger shutdown of any pending background processes
		cancel()

		if err := metricsSrv.Shutdown(context.Background()); err != nil {
			logger.Error("Failed to shutdown metrics server: %s", err)
		}

		// Shutdown the server and cancel context
		err := svc.Close()
		if err != nil {
			logger.Error(err.Error())
		}
	}()

	<-svcCtx.Done()
	return nil
}

func newKubeClient(cCtx *cli.Context) (dynamic.Interface, kubernetes.Interface, ctrlclient.Client, error) {
	kubeconfig := cCtx.String("kubeconfig")
	_, err := rest.InClusterConfig()
	if err == rest.ErrNotInCluster && kubeconfig == "" && os.Getenv("KUBECONIFG=") == "" {
		logger.Warn("No kube config provided, using fake kube client")
		return nil, fake.NewSimpleClientset(), nil, nil
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
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

func newKustoClient(endpoint string) (ingest.QueryClient, error) {
	kcsb := kusto.NewConnectionStringBuilder(endpoint)
	kcsb.WithDefaultAzureCredential()

	return kusto.New(kcsb)
}

func newUploader(kustoEndpoint, database, storageDir string, concurrentUploads int, defaultMapping storage.SchemaMapping) (adx.Uploader, error) {
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
		DefaultMapping:    defaultMapping,
	})
	return uploader, err
}

func newLogger() *log.Logger {
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
