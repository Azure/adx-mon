package main

import (
	"bytes"
	"context"
	"crypto/tls"
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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/urfave/cli/v2"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/ingestor"
	"github.com/Azure/adx-mon/ingestor/adx"
	"github.com/Azure/adx-mon/ingestor/clickhouse"
	"github.com/Azure/adx-mon/ingestor/cluster"
	ingestormetrics "github.com/Azure/adx-mon/ingestor/metrics"
	runner "github.com/Azure/adx-mon/ingestor/runner/shutdown"
	monhttp "github.com/Azure/adx-mon/pkg/http"
	"github.com/Azure/adx-mon/pkg/k8s"
	"github.com/Azure/adx-mon/pkg/limiter"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/scheduler"
	"github.com/Azure/adx-mon/pkg/service"
	adxtls "github.com/Azure/adx-mon/pkg/tls"
	"github.com/Azure/adx-mon/pkg/version"
	"github.com/Azure/adx-mon/schema"
	"github.com/Azure/adx-mon/storage"
	"github.com/Azure/azure-kusto-go/azkustodata"
)

func main() {
	if err := newApp().Run(os.Args); err != nil {
		logger.Fatal(err.Error())
	}
}

func newApp() *cli.App {
	return &cli.App{
		Name:  "ingestor",
		Usage: "adx-mon metrics ingestor",
		Flags: append([]cli.Flag{
			&cli.StringFlag{Name: "kubeconfig", Usage: "/etc/kubernetes/admin.conf"},
			&cli.StringFlag{Name: "namespace", Usage: "Namespace for peer discovery"},
			&cli.StringFlag{Name: "hostname", Usage: "Hostname of the current node"},
			&cli.StringFlag{Name: "region", Usage: "Current region"},
			&cli.StringFlag{Name: "storage-dir", Usage: "Directory to store WAL segments"},
			&cli.StringFlag{Name: "storage-backend", Usage: "Storage backend to use (adx or clickhouse)", EnvVars: []string{"INGESTOR_STORAGE_BACKEND"}, Value: string(storage.BackendADX)},
			&cli.StringSliceFlag{Name: "metrics-kusto-endpoints", Usage: "Storage endpoint in the format <db>=<endpoint> for metrics"},
			&cli.StringSliceFlag{Name: "logs-kusto-endpoints", Usage: "Storage endpoint in the format <db>=<endpoint> for OTLP logs"},
			&cli.StringSliceFlag{Name: "cluster-labels", Usage: "Labels used to identify and distinguish ingestor clusters. Format: <key>=<value>"},
			&cli.BoolFlag{Name: "disable-peer-transfer", Usage: "Disable segment transfers to peers"},
			&cli.IntFlag{Name: "uploads", Usage: "Number of concurrent uploads", Value: adx.ConcurrentUploads},
			&cli.UintFlag{Name: "max-connections", Usage: "Max number of concurrent connections allowed; connections beyond it are closed.  Each can hold up to 64 KB of request headers before its request is admitted.  0 for no limit", Value: 5000},
			&cli.Int64Flag{Name: "max-segment-size", Usage: "Maximum segment size in bytes", Value: 1024 * 1024 * 1024},
			&cli.Int64Flag{Name: "max-transfer-size", Usage: "Maximum segment size in bytes allowed for segment transfers", Value: 100 * 1024 * 1024},
			&cli.Int64Flag{Name: "max-disk-usage", Usage: "Maximum disk space usage allowed before signaling back-pressure", Value: 10 * 1024 * 1024 * 1024},
			&cli.Int64Flag{Name: "max-segment-count", Usage: "Maximum segment files allowed before signaling back-pressure", Value: 10000},
			&cli.Int64Flag{Name: "max-table-disk-usage", Usage: "Maximum WAL bytes per table; zero defaults to 10% of max-disk-usage"},
			&cli.Int64Flag{Name: "max-table-segment-count", Usage: "Maximum active and closed files per table; zero defaults to 10% of max-segment-count"},
			&cli.BoolFlag{Name: "disable-table-limits", Usage: "Disable default table quotas; explicit overrides still apply"},
			&cli.Int64Flag{Name: "memory-limit", Usage: "Go memory limit in bytes, a soft limit at which the garbage collector runs more often.  0 uses GOMEMLIMIT if set, otherwise 90% of the container's memory limit if it has one"},
			&cli.Int64Flag{Name: "max-intake-memory", Usage: "Maximum bytes buffered across concurrent transfer requests. 0 disables the ceiling", Value: 1024 * 1024 * 1024},
			&cli.BoolFlag{Name: "enforce-table-limits", Usage: "Reject writes that exceed a table budget. When unset, budgets are only measured and reported"},
			&cli.GenericFlag{Name: "table-limit", Value: &tableLimitValues{}, Usage: "Repeatable normalized table override as JSON: {\"database\":\"Logs\",\"table\":\"App\",\"max-bytes\":1073741824,\"max-segments\":1000}"},
			&cli.DurationFlag{Name: "max-transfer-age", Usage: "Maximum segment age of a segment before direct kusto upload", Value: 90 * time.Second},
			&cli.DurationFlag{Name: "max-segment-age", Usage: "Maximum segment age", Value: 5 * time.Minute},
			&cli.DurationFlag{Name: "unowned-segment-max-age", Usage: "Maximum segment age of tables owned by another ingestor, whose segments are transferred to the owner.  0 uses --max-segment-age", Value: ingestor.DefaultUnownedSegmentMaxAge},
			&cli.StringSliceFlag{Name: "drop-prefix", Usage: "Drop transfers that match the file prefix. Transfer filenames are in the form of DestinationDB_Table_..."},
			&cli.IntFlag{Name: "max-batch-segments", Usage: "Maximum number of segments per batch", Value: 25},
			&cli.BoolFlag{Name: "enable-wal-fsync", Usage: "Enable WAL fsync", Value: false},
			&cli.IntFlag{Name: "max-transfer-concurrency", Usage: "Maximum transfer requests in flight", Value: 50},
			&cli.IntFlag{Name: "max-transfers-per-table", Usage: "Maximum transfers of one table in flight.  Batches of a table waiting behind its running transfers are merged into one, so the owner receives fewer, larger transfers.  0 allows every transfer worker but the reserved ones"},
			&cli.DurationFlag{Name: "transfer-linger", Usage: "How long a closed segment of a table owned by another ingestor waits to be batched with others before it is transferred.  0 waits --upload-linger", Value: ingestor.DefaultTransferLinger},
			&cli.IntFlag{Name: "max-uploads-per-table", Usage: "Maximum queued uploads of one table in flight.  Batches of a table waiting behind its running uploads are merged into one, so Kusto receives fewer, larger ingestions.  0 allows every upload worker but the reserved ones"},
			&cli.IntFlag{Name: "max-upload-merge-segments", Usage: "Maximum segments in a batch made by merging the batches waiting to upload a table", Value: adx.DefaultMaxUploadMergeSegments},
			&cli.Int64Flag{Name: "max-upload-merge-size", Usage: "Maximum uncompressed size in bytes of a batch made by merging the batches waiting to upload a table", Value: adx.DefaultMaxUploadMergeSize},
			&cli.DurationFlag{Name: "upload-linger", Usage: "How long a closed segment of a table this ingestor owns waits to be batched with others before it is uploaded.  0 uses the default", Value: cluster.DefaultQueuedLinger},
			&cli.IntFlag{Name: "partition-size", Usage: "Maximum number of nodes in a partition", Value: 25},
			&cli.DurationFlag{Name: "slow-request-threshold", Usage: "Threshold for slow requests. Set to 0 to disable.", Value: 10 * time.Second},

			&cli.StringFlag{Name: "ca-cert", Usage: "CA certificate file"},
			&cli.StringFlag{Name: "key", Usage: "Server key file"},
			&cli.BoolFlag{Name: "insecure-skip-verify", Usage: "Skip TLS verification"},
		}, realtimeCLIFlags()...),

		Action: func(ctx *cli.Context) error {
			return realMain(ctx)
		},

		Version: version.String(),
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
	applyMemoryLimit(ctx.Int64("memory-limit"))

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
	unownedSegmentMaxAge := ctx.Duration("unowned-segment-max-age")
	maxTransferSize = ctx.Int64("max-transfer-size")
	maxTransferAge = ctx.Duration("max-transfer-age")
	maxSegmentCount := ctx.Int64("max-segment-count")
	dropPrefixes := ctx.StringSlice("drop-prefix")
	maxDiskUsage := ctx.Int64("max-disk-usage")
	tableIsolation, err := parseTableIsolationOpts(ctx)
	if err != nil {
		return err
	}
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
	maxTransfersPerTable := ctx.Int("max-transfers-per-table")
	transferLinger := ctx.Duration("transfer-linger")
	uploadLinger := ctx.Duration("upload-linger")
	uploadSchedule := adx.UploaderOpts{
		ConcurrentUploads:  concurrentUploads,
		MaxUploadsPerTable: ctx.Int("max-uploads-per-table"),
		MaxMergeSegments:   ctx.Int("max-upload-merge-segments"),
		MaxMergeSize:       ctx.Int64("max-upload-merge-size"),
	}
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

	// Build server TLS config
	var (
		serverTLSConfig *tls.Config
		cert            tls.Certificate
	)
	if cacert != "" && key != "" {
		certPEM, err := os.ReadFile(cacert)
		if err != nil {
			logger.Fatalf("Failed to read cert file: %s", err)
		}
		keyPEM, err := os.ReadFile(key)
		if err != nil {
			logger.Fatalf("Failed to read key file: %s", err)
		}
		c, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			logger.Fatalf("Failed to parse TLS key pair: %s", err)
		}
		cert = c
		logger.Infof("Using TLS from files cert=%s key=%s", cacert, key)
	} else if cacert == "" && key == "" {
		logger.Warnf("Using fake TLS credentials (not for production use!)")
		certBytes, keyBytes, err := adxtls.NewFakeTLSCredentials()
		if err != nil {
			logger.Fatalf("Failed to create fake TLS credentials: %s", err)
		}
		c, err := tls.X509KeyPair(certBytes, keyBytes)
		if err != nil {
			logger.Fatalf("Failed to build in-memory TLS key pair: %s", err)
		}
		cert = c
		insecureSkipVerify = true
	} else {
		// If either cacert or key is provided, then it is assumed that this code is
		// running in production and we bail out.
		logger.Fatalf("Both --ca-cert and --key are required")
	}
	serverTLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}

	if storageDir == "" {
		logger.Fatalf("--storage-dir is required")
	}

	backendRaw := ctx.String("storage-backend")
	backend, err := storage.ParseBackend(backendRaw)
	if err != nil {
		logger.Fatalf("Invalid storage backend: %s", err)
	}
	logger.Infof("Ingestor storage backend: %s", backend)

	metricsEndpoints := ctx.StringSlice("metrics-kusto-endpoints")
	logsEndpoints := ctx.StringSlice("logs-kusto-endpoints")

	realtimeCfg, err := newRealtimeConfig(ctx, metricsEndpoints, logsEndpoints, maxDiskUsage, backend)
	if err != nil {
		logger.Fatalf("Invalid realtime configuration: %s", err)
	}
	if realtimeCfg.Policy.HasRealtime() {
		logger.Infof("Realtime ingestion configured for tables: %v", realtimeCfg.Policy.RealtimeTables())
	}

	allowedDatabases := make([]string, 0, len(metricsEndpoints)+len(logsEndpoints))
	var (
		metricsDatabases []string
		logsDatabases    []string
		metricsKustoCli  []ingestormetrics.StatementExecutor
		logsKustoCli     []ingestormetrics.StatementExecutor
		uploader         ingestor.Uploader
	)

	streamingSlots := realtimeCfg.newStreamingSlots()
	if len(streamingSlots) > 0 {
		prometheus.MustRegister(adx.NewStreamingSlotsCollector(streamingSlots))
	}

	switch backend {
	case storage.BackendADX:
		metricsUploaders, md, err := newUploaders(
			metricsEndpoints, storageDir, uploadSchedule,
			schema.DefaultMetricsMapping, adx.PromMetrics, realtimeCfg, streamingSlots)
		if err != nil {
			logger.Fatalf("Failed to create metrics uploader: %s", err)
		}
		metricsDatabases = md
		logsUploaders, ld, err := newUploaders(
			logsEndpoints, storageDir, uploadSchedule,
			schema.DefaultLogsMapping, adx.OTLPLogs, realtimeCfg, streamingSlots)
		if err != nil {
			logger.Fatalf("Failed to create logs uploader: %s", err)
		}
		logsDatabases = ld

		uploadDispatcher := adx.NewDispatcher(append(logsUploaders, metricsUploaders...))
		if err := uploadDispatcher.Open(svcCtx); err != nil {
			logger.Fatalf("Failed to start upload dispatcher: %s", err)
		}
		uploader = uploadDispatcher
		defer uploadDispatcher.Close()

		for _, cli := range metricsUploaders {
			metricsKustoCli = append(metricsKustoCli, cli)
		}
		for _, cli := range logsUploaders {
			logsKustoCli = append(logsKustoCli, cli)
		}

	case storage.BackendClickHouse:
		metricsUploaders, md, err := newClickHouseUploaders(metricsEndpoints)
		if err != nil {
			logger.Fatalf("Failed to create ClickHouse metrics uploader: %s", err)
		}
		logsUploaders, ld, err := newClickHouseUploaders(logsEndpoints)
		if err != nil {
			logger.Fatalf("Failed to create ClickHouse logs uploader: %s", err)
		}

		metricsDatabases = md
		logsDatabases = ld

		combined := append(metricsUploaders, logsUploaders...)
		chDispatcher := clickhouse.NewDispatcher(logger.Logger(), combined)
		if err := chDispatcher.Open(svcCtx); err != nil {
			logger.Fatalf("Failed to start ClickHouse dispatcher: %s", err)
		}
		uploader = chDispatcher
		defer chDispatcher.Close()

	default:
		logger.Fatalf("Unsupported storage backend %q", backend)
	}

	if uploader == nil {
		logger.Fatalf("No uploader configured for backend %s", backend)
	}

	allowedDatabases = append(allowedDatabases, metricsDatabases...)
	allowedDatabases = append(allowedDatabases, logsDatabases...)

	svcOpts := ingestor.ServiceOpts{
		K8sCli:                       k8scli,
		K8sCtrlCli:                   ctrlCli,
		LogsKustoCli:                 logsKustoCli,
		MetricsKustoCli:              metricsKustoCli,
		MetricsDatabases:             metricsDatabases,
		AllowedDatabase:              allowedDatabases,
		LogsDatabases:                logsDatabases,
		Namespace:                    namespace,
		Hostname:                     hostname,
		Region:                       region,
		StorageDir:                   storageDir,
		Uploader:                     uploader,
		DisablePeerTransfer:          disablePeerTransfer,
		PartitionSize:                partitionSize,
		MaxSegmentSize:               maxSegmentSize,
		MaxSegmentAge:                maxSegmentAge,
		UnownedSegmentMaxAge:         unownedSegmentMaxAge,
		MaxTransferSize:              maxTransferSize,
		MaxTransferAge:               maxTransferAge,
		MaxSegmentCount:              maxSegmentCount,
		MaxDiskUsage:                 maxDiskUsage,
		MaxBatchSegments:             maxBatchSegments,
		ConcurrentUploads:            concurrentUploads,
		EnableWALFsync:               enableWALFsync,
		MaxTransferConcurrency:       maxTransferConcurrency,
		MaxTransfersPerTable:         maxTransfersPerTable,
		TransferLinger:               transferLinger,
		UploadLinger:                 uploadLinger,
		InsecureSkipVerify:           insecureSkipVerify,
		DropFilePrefixes:             dropPrefixes,
		SlowRequestThreshold:         slowRequestThreshold.Seconds(),
		ClusterLabels:                makeClusterLabels(ctx),
		StorageBackend:               backend,
		Realtime:                     realtimeCfg.serviceOpts(),
		QueuedReservedWorkersPercent: realtimeCfg.QueuedReservedWorkersPercent,
		PeerListeners:                peerListeners(streamingSlots),
	}
	tableIsolation.apply(&svcOpts)
	svc, err := ingestor.NewService(svcOpts)
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

	srv := newTransferServer(mux)
	srv.ErrorLog = newLogger()

	go func() {
		// Under high connection load and when the server is doing a lot of IO, this
		// can cause the server to be unresponsive.  This pins the accept goroutine
		// to a single CPU to reduce context switching and improve performance.
		runtime.LockOSThread()
		if err := pinToCPU(0); err != nil {
			logger.Warnf("Failed to pin to CPU: %s", err)
		}

		tlsListener := tls.NewListener(l, serverTLSConfig)
		if err := srv.Serve(tlsListener); err != nil {
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

	// Only start the shutdown runner if running in a cluster
	if _, err := rest.InClusterConfig(); err == nil {
		go func() {
			sd := runner.NewShutDownRunnerForNamespace(k8scli, srv, svc, namespace)
			scheduler.RunForever(svcCtx, time.Minute, sd)
		}()
	}

	// Block until svc.Close() has drained, so the process does not exit mid-upload.
	service.WaitForShutdown(sc, func(sig os.Signal) {
		// Bound each HTTP shutdown so a stuck connection cannot hang past the pod's grace period.
		const shutdownTimeout = 30 * time.Second

		// Stop receiving new samples
		srvCtx, srvCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer srvCancel()
		if err := srv.Shutdown(srvCtx); err != nil {
			logger.Errorf("Failed to shutdown server: %s", err)
		}

		// Disable writes for internal processes (metrics)
		if err := svc.DisableWrites(); err != nil {
			logger.Errorf("Failed to disable writes: %s", err)
		}

		logger.Infof("Received signal %s, uploading pending segments...", sig.String())

		// Trigger shutdown of any pending background processes
		cancel()

		metricsCtx, metricsCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer metricsCancel()
		if err := metricsSrv.Shutdown(metricsCtx); err != nil {
			logger.Errorf("Failed to shutdown metrics server: %s", err)
		}

		// Shutdown the server and cancel context
		err := svc.Close()
		if err != nil {
			logger.Error(err.Error())
		}
	})
	return nil
}

func newKubeClient(cCtx *cli.Context) (dynamic.Interface, kubernetes.Interface, ctrlclient.Client, error) {
	kubeconfig := cCtx.String("kubeconfig")
	_, err := rest.InClusterConfig()
	if err == rest.ErrNotInCluster && kubeconfig == "" && os.Getenv("KUBECONIFG=") == "" {
		logger.Warnf("No kube config provided, using fake kube client")
		return nil, fake.NewSimpleClientset(), nil, nil
	}

	config, err := k8s.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to find kube config [%s]: %v", kubeconfig, err)
	}

	if config.UserAgent == "" {
		config.UserAgent = fmt.Sprintf("adx-mon-ingestor/%s", version.String())
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

func newKustoClient(endpoint string) (*azkustodata.Client, error) {
	kcsb := azkustodata.NewConnectionStringBuilder(endpoint)

	if strings.HasPrefix(endpoint, "https://") {
		kcsb.WithDefaultAzureCredential()
	}
	// Create the SDK client first to preserve any defaults the SDK sets on its http.Client.
	c, err := azkustodata.New(kcsb)
	if err != nil {
		return nil, err
	}
	// Wrap the existing http.Client with our error-logging RoundTripper to capture transport/HTTP failures.
	base := c.HttpClient()
	monhttp.WithLogging(base)
	return c, nil
}

func parseStorageEndpoint(endpoint string) (string, string, error) {
	if !strings.Contains(endpoint, "=") {
		return "", "", fmt.Errorf("invalid endpoint: %s", endpoint)
	}

	split := strings.SplitN(endpoint, "=", 2)
	database := strings.TrimSpace(split[0])
	target := strings.TrimSpace(split[1])

	if database == "" {
		return "", "", fmt.Errorf("database name is required in %q", endpoint)
	}
	if target == "" {
		return "", "", fmt.Errorf("endpoint address is required for database %q", database)
	}
	return target, database, nil
}

// newUploaders returns an uploader for each endpoint, scheduled as schedule says.
func newUploaders(endpoints []string, storageDir string, schedule adx.UploaderOpts,
	defaultMapping schema.SchemaMapping, sampleType adx.SampleType,
	realtimeCfg *realtimeConfig, streamingSlots map[string]*adx.StreamingSlots) ([]adx.Uploader, []string, error) {

	var (
		uploaders           []adx.Uploader
		uploadDatabaseNames []string
	)

	if len(endpoints) == 0 {
		var fakeName string
		switch sampleType {
		case adx.PromMetrics:
			fakeName = "FakeMetrics"
		case adx.OTLPLogs:
			fakeName = "FakeLogs"
		default:
			fakeName = "Fake"
		}
		logger.Warnf("No Kusto endpoint provided, using fake uploader %s", fakeName)
		uploader := adx.NewFakeUploader(fakeName)
		uploaders = append(uploaders, uploader)
		uploadDatabaseNames = append(uploadDatabaseNames, uploader.Database())
		return uploaders, uploadDatabaseNames, nil
	}

	for _, endpoint := range endpoints {
		addr, database, err := parseStorageEndpoint(endpoint)
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
			ConcurrentUploads: schedule.ConcurrentUploads,
			DefaultMapping:    defaultMapping,
			SampleType:        sampleType,

			QueuedReservedWorkersPercent: realtimeCfg.QueuedReservedWorkersPercent,
			Realtime:                     realtimeCfg.uploadOpts(database, addr, streamingSlots),

			MaxUploadsPerTable: schedule.MaxUploadsPerTable,
			MaxMergeSegments:   schedule.MaxMergeSegments,
			MaxMergeSize:       schedule.MaxMergeSize,
		}))

		uploadDatabaseNames = append(uploadDatabaseNames, database)
	}
	return uploaders, uploadDatabaseNames, nil
}

func newClickHouseUploaders(endpoints []string) ([]clickhouse.Uploader, []string, error) {
	if len(endpoints) == 0 {
		logger.Warnf("No ClickHouse endpoint provided for this stream; uploads will be skipped")
		return nil, nil, nil
	}

	uploaders := make([]clickhouse.Uploader, 0, len(endpoints))
	databases := make([]string, 0, len(endpoints))

	for _, endpoint := range endpoints {
		dsn, database, err := parseStorageEndpoint(endpoint)
		if err != nil {
			return nil, nil, fmt.Errorf("parse clickhouse endpoint %q: %w", endpoint, err)
		}

		cfg := clickhouse.Config{Database: database, DSN: dsn}
		uploader, err := clickhouse.NewUploader(cfg, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("init clickhouse uploader for database %q: %w", database, err)
		}

		uploaders = append(uploaders, uploader)
		databases = append(databases, database)
	}

	return uploaders, databases, nil
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

// transferMaxHeaderBytes bounds the headers of a request to the transfer endpoint.  Every connection can hold this
// much before its request is admitted, so it sets the memory --max-connections allows.  Transfers carry their filename
// in the query and a few short headers.
const transferMaxHeaderBytes = 64 << 10

// newTransferServer returns the server for the ingestor's transfer endpoint.
func newTransferServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:        handler,
		MaxHeaderBytes: transferMaxHeaderBytes,
		// Close idle connections fairly often to establish new connections through the load balancer
		// so that long-lived connections don't stay pinned to the same node indefinitely.
		IdleTimeout: 5 * time.Minute,
		// Transfer bodies are bounded by a stall timeout in the handler instead of a fixed ReadTimeout, so large
		// transfers that keep arriving are not cut off.
		ReadHeaderTimeout: 10 * time.Second,
	}
}
