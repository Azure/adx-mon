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
	"runtime"
	"strings"
	"syscall"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/ingestor"
	"github.com/Azure/adx-mon/ingestor/adx"
	runner "github.com/Azure/adx-mon/ingestor/runner/shutdown"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/limiter"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/scheduler"
	"github.com/Azure/adx-mon/pkg/tls"
	"github.com/Azure/adx-mon/pkg/version"
	"github.com/Azure/adx-mon/schema"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/urfave/cli/v2"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func main() {
	app := &cli.App{
		Name:  "ingestor",
		Usage: "adx-mon metrics ingestor",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "kubeconfig", Usage: "/etc/kubernetes/admin.conf"},
			&cli.StringFlag{Name: "namespace", Usage: "Namespace for peer discovery"},
			&cli.StringFlag{Name: "hostname", Usage: "Hostname of the current node"},
			&cli.StringFlag{Name: "region", Usage: "Current region"},
			&cli.StringFlag{Name: "storage-dir", Usage: "Directory to store WAL segments"},
			&cli.StringSliceFlag{Name: "metrics-kusto-endpoints", Usage: "Kusto endpoint in the format of <db>=<endpoint> for metrics storage"},
			&cli.StringSliceFlag{Name: "logs-kusto-endpoints", Usage: "Kusto endpoint in the format of <db>=<endpoint>, handles OTLP logs"},
			&cli.StringSliceFlag{Name: "cluster-labels", Usage: "Labels used to identify and distinguish ingestor clusters. Format: <key>=<value>"},
			&cli.BoolFlag{Name: "disable-peer-transfer", Usage: "Disable segment transfers to peers"},
			&cli.IntFlag{Name: "uploads", Usage: "Number of concurrent uploads", Value: adx.ConcurrentUploads},
			&cli.UintFlag{Name: "max-connections", Usage: "Max number of concurrent connection allowed.  0 for no limit", Value: 1000},
			&cli.Int64Flag{Name: "max-segment-size", Usage: "Maximum segment size in bytes", Value: 1024 * 1024 * 1024},
			&cli.Int64Flag{Name: "max-transfer-size", Usage: "Maximum segment size in bytes allowed for segment transfers", Value: 100 * 1024 * 1024},
			&cli.Int64Flag{Name: "max-disk-usage", Usage: "Maximum disk space usage allowed before signaling back-pressure", Value: 10 * 1024 * 1024 * 1024},
			&cli.Int64Flag{Name: "max-segment-count", Usage: "Maximum segment files allowed before signaling back-pressure", Value: 10000},
			&cli.DurationFlag{Name: "max-transfer-age", Usage: "Maximum segment age of a segment before direct kusto upload", Value: 90 * time.Second},
			&cli.DurationFlag{Name: "max-segment-age", Usage: "Maximum segment age", Value: 5 * time.Minute},
			&cli.StringSliceFlag{Name: "drop-prefix", Usage: "Drop transfers that match the file prefix. Transfer filenames are in the form of DestinationDB_Table_..."},
			&cli.IntFlag{Name: "max-batch-segments", Usage: "Maximum number of segments per batch", Value: 25},
			&cli.BoolFlag{Name: "enable-wal-fsync", Usage: "Enable WAL fsync", Value: false},
			&cli.IntFlag{Name: "max-transfer-concurrency", Usage: "Maximum transfer requests in flight", Value: 50},
			&cli.IntFlag{Name: "partition-size", Usage: "Maximum number of nodes in a partition", Value: 25},
			&cli.DurationFlag{Name: "slow-request-threshold", Usage: "Threshold for slow requests. Set to 0 to disable.", Value: 10 * time.Second},

			&cli.StringFlag{Name: "ca-cert", Usage: "CA certificate file"},
			&cli.StringFlag{Name: "key", Usage: "Server key file"},
			&cli.BoolFlag{Name: "insecure-skip-verify", Usage: "Skip TLS verification"},
		},

		Action: func(ctx *cli.Context) error {
			return realMain(ctx)
		},

		Version: version.String(),
	}

	if err := app.Run(os.Args); err != nil {
		logger.Fatal(err.Error())
	}
}

func realMain(ctx *cli.Context) error {
	logger.Infof("%s version:%s", os.Args[0], version.String())

	svcCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheme := clientgoscheme.Scheme
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return err
	}
	if err := v1.AddToScheme(scheme); err != nil {
		return err
	}

	_, k8scli, ctrlCli, err := newKubeClient(ctx)
	if err != nil {
		return err
	}

	runtime.MemProfileRate = 4096
	runtime.SetBlockProfileRate(int(1 * time.Second))
	runtime.SetMutexProfileFraction(1)

	var (
		storageDir                              string
		cacert, key                             string
		insecureSkipVerify, disablePeerTransfer bool
		concurrentUploads                       int
		maxConns                                int
		maxSegmentSize, maxTransferSize         int64
		maxSegmentAge, maxTransferAge           time.Duration
	)
	storageDir = ctx.String("storage-dir")
	concurrentUploads = ctx.Int("uploads")
	maxSegmentSize = ctx.Int64("max-segment-size")
	maxSegmentAge = ctx.Duration("max-segment-age")
	maxTransferSize = ctx.Int64("max-transfer-size")
	maxTransferAge = ctx.Duration("max-transfer-age")
	maxSegmentCount := ctx.Int64("max-segment-count")
	dropPrefixes := ctx.StringSlice("drop-prefix")
	maxDiskUsage := ctx.Int64("max-disk-usage")
	maxBatchSegments := ctx.Int("max-batch-segments")
	partitionSize := ctx.Int("partition-size")
	maxConns = int(ctx.Uint("max-connections"))
	cacert = ctx.String("ca-cert")
	key = ctx.String("key")
	insecureSkipVerify = ctx.Bool("insecure-skip-verify")
	namespace := ctx.String("namespace")
	hostname := ctx.String("hostname")
	region := ctx.String("region")
	disablePeerTransfer = ctx.Bool("disable-peer-transfer")
	maxTransferConcurrency := ctx.Int("max-transfer-concurrency")
	enableWALFsync := ctx.Bool("enable-wal-fsync")
	slowRequestThreshold := ctx.Duration("slow-request-threshold")

	if namespace == "" {
		nsBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
		if err == nil {
			namespace = strings.TrimSpace(string(nsBytes))
		}
	}

	if hostname == "" {
		hostname, err = os.Hostname()
		if err != nil {
			logger.Errorf("Failed to get hostname: %s", err)
		}
	}

	if cacert != "" || key != "" {
		if cacert == "" || key == "" {
			logger.Fatalf("Both --ca-cert and --key are required")
		}
	} else {
		logger.Warnf("Using fake TLS credentials (not for production use!)")
		certBytes, keyBytes, err := tls.NewFakeTLSCredentials()
		if err != nil {
			logger.Fatalf("Failed to create fake TLS credentials: %s", err)
		}

		certFile, err := os.CreateTemp("", "cert")
		if err != nil {
			logger.Fatalf("Failed to create cert temp file: %s", err)
		}

		if _, err := certFile.Write(certBytes); err != nil {
			logger.Fatalf("Failed to write cert temp file: %s", err)
		}

		keyFile, err := os.CreateTemp("", "key")
		if err != nil {
			logger.Fatalf("Failed to create key temp file: %s", err)
		}

		if _, err := keyFile.Write(keyBytes); err != nil {
			logger.Fatalf("Failed to write key temp file: %s", err)
		}

		cacert = certFile.Name()
		key = keyFile.Name()
		insecureSkipVerify = true

		if err := certFile.Close(); err != nil {
			logger.Fatalf("Failed to close cert temp file: %s", err)
		}

		if err := keyFile.Close(); err != nil {
			logger.Fatalf("Failed to close key temp file: %s", err)
		}

		defer func() {
			os.Remove(certFile.Name())
			os.Remove(keyFile.Name())
		}()
	}

	logger.Infof("Using TLS ca-cert=%s key=%s", cacert, key)
	if storageDir == "" {
		logger.Fatalf("--storage-dir is required")
	}

	var (
		allowedDatabases                []string
		metricsUploaders, logsUploaders []adx.Uploader
		metricsDatabases, logsDatabases []string
	)

	metricsKusto := ctx.StringSlice("metrics-kusto-endpoints")
	if len(metricsKusto) > 0 {
		metricsUploaders, metricsDatabases, err = newUploaders(
			metricsKusto, storageDir, concurrentUploads,
			schema.DefaultMetricsMapping, adx.PromMetrics)
		if err != nil {
			logger.Fatalf("Failed to create uploader: %s", err)
		}
	} else {
		logger.Warnf("No kusto endpoint provided, using fake metrics uploader")
		uploader := adx.NewFakeUploader("FakeMetrics")
		metricsUploaders = append(metricsUploaders, uploader)
		metricsDatabases = append(metricsDatabases, uploader.Database())
	}

	logsKusto := ctx.StringSlice("logs-kusto-endpoints")
	if len(logsKusto) > 0 {
		logsUploaders, logsDatabases, err = newUploaders(
			logsKusto, storageDir, concurrentUploads,
			schema.DefaultLogsMapping, adx.OTLPLogs)
		if err != nil {
			logger.Fatalf("Failed to create uploaders for OTLP logs: %s", err)
		}
	} else {
		logger.Warnf("No kusto endpoint provided, using fake logs uploader")
		uploader := adx.NewFakeUploader("FakeLogs")
		logsUploaders = append(logsUploaders, uploader)
		logsDatabases = append(logsDatabases, uploader.Database())
	}

	allowedDatabases = append(allowedDatabases, metricsDatabases...)
	allowedDatabases = append(allowedDatabases, logsDatabases...)

	uploadDispatcher := adx.NewDispatcher(append(logsUploaders, metricsUploaders...))
	if err := uploadDispatcher.Open(svcCtx); err != nil {
		logger.Fatalf("Failed to start upload dispatcher: %s", err)
	}
	defer uploadDispatcher.Close()

	var metricsKustoCli []metrics.StatementExecutor
	for _, cli := range metricsUploaders {
		metricsKustoCli = append(metricsKustoCli, cli)
	}

	var logsKustoCli []metrics.StatementExecutor
	for _, cli := range logsUploaders {
		logsKustoCli = append(logsKustoCli, cli)
	}

	svc, err := ingestor.NewService(ingestor.ServiceOpts{
		K8sCli:                 k8scli,
		K8sCtrlCli:             ctrlCli,
		LogsKustoCli:           logsKustoCli,
		MetricsKustoCli:        metricsKustoCli,
		MetricsDatabases:       metricsDatabases,
		AllowedDatabase:        metricsDatabases,
		LogsDatabases:          logsDatabases,
		Namespace:              namespace,
		Hostname:               hostname,
		Region:                 region,
		StorageDir:             storageDir,
		Uploader:               uploadDispatcher,
		DisablePeerTransfer:    disablePeerTransfer,
		PartitionSize:          partitionSize,
		MaxSegmentSize:         maxSegmentSize,
		MaxSegmentAge:          maxSegmentAge,
		MaxTransferSize:        maxTransferSize,
		MaxTransferAge:         maxTransferAge,
		MaxSegmentCount:        maxSegmentCount,
		MaxDiskUsage:           maxDiskUsage,
		MaxBatchSegments:       maxBatchSegments,
		EnableWALFsync:         enableWALFsync,
		MaxTransferConcurrency: maxTransferConcurrency,
		InsecureSkipVerify:     insecureSkipVerify,
		DropFilePrefixes:       dropPrefixes,
		SlowRequestThreshold:   slowRequestThreshold.Seconds(),
		ClusterLabels:          makeClusterLabels(ctx),
	})
	if err != nil {
		logger.Fatalf("Failed to create service: %s", err)
	}
	if err := svc.Open(svcCtx); err != nil {
		logger.Fatalf("Failed to start service: %s", err)
	}

	l, err := net.Listen("tcp", ":9090")
	if err != nil {
		logger.Fatalf("Failed to listen: %s", err)
	}
	if maxConns > 0 {
		logger.Infof("Limiting connections to %d", maxConns)
		l = limiter.LimitListener(l, maxConns)
	}
	defer l.Close()

	logger.Infof("Listening at %s", ":9090")
	mux := http.NewServeMux()
	mux.HandleFunc("/transfer", svc.HandleTransfer)
	mux.HandleFunc("/readyz", svc.HandleReady)

	logger.Infof("Metrics Listening at %s", ":9091")
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())
	metricsMux.HandleFunc("/debug/pprof/", pprof.Index)
	metricsMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	metricsMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	metricsMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	metricsMux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	metricsMux.HandleFunc("/debug/store", svc.HandleDebugStore)

	srv := &http.Server{
		Handler: mux,
		// Close idle connections fairly often to establish new connections through the load balancer
		// so that long-lived connections don't stay pinned to the same node indefinitely.
		IdleTimeout: 5 * time.Minute,
	}
	srv.ErrorLog = newLogger()

	go func() {
		// Under high connection load and when the server is doing a lot of IO, this
		// can cause the server to be unresponsive.  This pins the accept goroutine
		// to a single CPU to reduce context switching and improve performance.
		runtime.LockOSThread()
		if err := pinToCPU(0); err != nil {
			logger.Warnf("Failed to pin to CPU: %s", err)
		}

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
			logger.Errorf("Failed to disable writes: %s", err)
		}

		logger.Infof("Received signal %s, uploading pending segments...", sig.String())

		// Trigger shutdown of any pending background processes
		cancel()

		if err := metricsSrv.Shutdown(context.Background()); err != nil {
			logger.Errorf("Failed to shutdown metrics server: %s", err)
		}

		// Shutdown the server and cancel context
		err := svc.Close()
		if err != nil {
			logger.Error(err.Error())
		}
	}()

	// Only start the shutdown runner if running in a cluster
	if _, err := rest.InClusterConfig(); err == nil {
		go func() {
			sd := runner.NewShutDownRunner(k8scli, srv, svc)
			scheduler.RunForever(svcCtx, time.Minute, sd)
		}()
	}

	<-svcCtx.Done()
	return nil
}

func newKubeClient(cCtx *cli.Context) (dynamic.Interface, kubernetes.Interface, ctrlclient.Client, error) {
	kubeconfig := cCtx.String("kubeconfig")
	_, err := rest.InClusterConfig()
	if err == rest.ErrNotInCluster && kubeconfig == "" && os.Getenv("KUBECONIFG=") == "" {
		logger.Warnf("No kube config provided, using fake kube client")
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

func newKustoClient(endpoint string) (*kusto.Client, error) {
	kcsb := kusto.NewConnectionStringBuilder(endpoint)

	if strings.HasPrefix(endpoint, "https://") {
		kcsb.WithDefaultAzureCredential()
	}

	return kusto.New(kcsb)
}

func parseKustoEndpoint(kustoEndpoint string) (string, string, error) {
	if !strings.Contains(kustoEndpoint, "=") {
		return "", "", fmt.Errorf("invalid kusto endpoint: %s", kustoEndpoint)
	}

	split := strings.Split(kustoEndpoint, "=")
	database := split[0]
	addr := split[1]

	if database == "" {
		return "", "", fmt.Errorf("-db is required")
	}
	return addr, database, nil
}

func newUploaders(endpoints []string, storageDir string, concurrentUploads int,
	defaultMapping schema.SchemaMapping, sampleType adx.SampleType) ([]adx.Uploader, []string, error) {

	var uploaders []adx.Uploader
	var uploadDatabaseNames []string
	for _, endpoint := range endpoints {
		addr, database, err := parseKustoEndpoint(endpoint)
		if err != nil {
			return nil, nil, err
		}

		client, err := newKustoClient(addr)
		if err != nil {
			return nil, nil, err
		}
		uploaders = append(uploaders, adx.NewUploader(client, adx.UploaderOpts{
			StorageDir:        storageDir,
			Database:          database,
			ConcurrentUploads: concurrentUploads,
			DefaultMapping:    defaultMapping,
			SampleType:        sampleType,
		}))

		uploadDatabaseNames = append(uploadDatabaseNames, database)
	}
	return uploaders, uploadDatabaseNames, nil
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
	if bytes.Contains(data, tlsHandshakeError) && bytes.Contains(data, ioEOFError) || bytes.Contains(data, []byte("connection reset by peer")) {
		return len(data), nil
	}
	return a.Writer.Write(data)
}

// makeClusterLabels processes the --cluster-labels CLI arguments into a map for SummaryRule criteria matching and templating.
//
// This function stores cluster labels as-is for criteria matching. Template substitution happens later
// in applySubstitutions() where underscore prefixes are added to match SummaryRule placeholders.
//
// CLI Input Format: --cluster-labels=<key>=<value>
// Examples:
//
//	--cluster-labels=environment=production --cluster-labels=region=eastus
//	--cluster-labels=datacenter=west --cluster-labels=cluster_id=prod-01
//
// Criteria Matching Process:
// 1. Keys are stored without modification for case-insensitive criteria matching
// 2. SummaryRule criteria like {"region": ["eastus"]} match against {"region": "eastus"}
//
// Template Substitution Process:
// 1. Keys get underscore prefixes during applySubstitutions() in ingestor/adx/tasks.go
// 2. In SummaryRule bodies, placeholders like "_region" get replaced with quoted values like "eastus"
//
// Example transformation:
//
//	Input:  --cluster-labels=region=eastus --cluster-labels=environment=production
//	Output: map[string]string{"region": "eastus", "environment": "production"}
//
// SummaryRule usage example:
//
//	Body: | where Region == "_region" and Environment == "_environment"
//	Result: | where Region == "eastus" and Environment == "production"
//
// SECURITY NOTE: This templating directly modifies KQL queries. Incorrect key/value handling
// could lead to query injection or unintended data access. The underscore prefix ensures
// template keys are clearly distinguished from regular KQL syntax.
func makeClusterLabels(ctx *cli.Context) map[string]string {
	clusterLabels := make(map[string]string)
	for _, label := range ctx.StringSlice("cluster-labels") {
		split := strings.SplitN(label, "=", 2)
		if len(split) != 2 {
			logger.Fatalf("Invalid cluster label format: %s, expected <key>=<value>", label)
		}

		key := split[0]
		value := split[1]

		// Store the key-value pair as-is for criteria matching
		// The underscore prefix will be added during template substitution
		clusterLabels[key] = value
	}
	return clusterLabels
}
