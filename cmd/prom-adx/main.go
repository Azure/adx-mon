package main

import (
	"context"
	"flag"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Azure/adx-mon/adx"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/adx-mon/storage"
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
	svcCtx, cancel := context.WithCancel(context.Background())

	runtime.MemProfileRate = 4096
	runtime.SetBlockProfileRate(int(1 * time.Second))
	runtime.SetMutexProfileFraction(1)

	var (
		storageDir, kustoEndpoint, database string
		concurrentUploads                   int
		maxSegmentSize                      int64
		maxSegmentAge                       time.Duration
		useCliAuth                          bool
		staticColumns                       strSliceFag
	)
	flag.StringVar(&storageDir, "storage-dir", "", "Directory to storage WAL segments")
	flag.StringVar(&kustoEndpoint, "kusto-endpoint", "", "Kusto cluster endpoint")
	flag.StringVar(&database, "db", "", "Kusto DB")
	flag.IntVar(&concurrentUploads, "uploads", adx.ConcurrentUploads, "Max concurrent uploads")
	flag.Int64Var(&maxSegmentSize, "max-segment-size", 1024*1024*1024, "Max segment file size")
	flag.DurationVar(&maxSegmentAge, "max-segment-age", 5*time.Minute, "Max segment file age")
	flag.BoolVar(&useCliAuth, "use-cli-auth", false, "Use az cli auth")
	flag.Var(&staticColumns, "dimensions", "Static column=value fields to add to all records")

	flag.Parse()

	if storageDir == "" {
		logger.Fatal("-storage-dir is required")
	}

	if kustoEndpoint == "" {
		logger.Fatal("-kusto-endpoint is required")
	}
	if database == "" {
		logger.Fatal("-db is required")
	}

	for _, v := range staticColumns {
		fields := strings.Split(v, "=")
		if len(fields) != 2 {
			logger.Fatal("invalid dimension: %s", v)
		}

		storage.AddDefaultMapping(fields[0], fields[1])
	}

	svc, err := NewService(ServiceOpts{
		StorageDir:        storageDir,
		KustoEndpoint:     kustoEndpoint,
		Database:          database,
		ConcurrentUploads: concurrentUploads,
		MaxSegmentSize:    maxSegmentSize,
		MaxSegmentAge:     maxSegmentAge,
		UseCLIAuth:        useCliAuth,
	})
	if err != nil {
		logger.Fatal("Failed to create service: %s", err)
	}
	if err := svc.Open(svcCtx); err != nil {
		logger.Fatal("Failed to start service: %s", err)
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

	http.HandleFunc("/transfer", svc.HandleTransfer)
	http.HandleFunc("/receive", svc.HandleReceive)
	logger.Info("Listening at :9090")
	if err := http.ListenAndServe(":9090", nil); err != nil {
		logger.Fatal("ListenAndServe returned error: %s", err)
	}
	<-svcCtx.Done()
}
