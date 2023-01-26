package main

import (
	"flag"
	promingest "github.com/Azure/adx-mon"
	"github.com/Azure/adx-mon/adx"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/adx-mon/storage"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"strings"
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

	svc, err := promingest.NewService(promingest.ServiceOpts{
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
	if err := svc.Open(); err != nil {
		logger.Fatal("Failed to start service: %s", err)
	}

	http.HandleFunc("/transfer", svc.HandleTransfer)
	http.HandleFunc("/receive", svc.HandleReceive)
	logger.Info("Listening at :9090")
	if err := http.ListenAndServe(":9090", nil); err != nil {
		logger.Fatal("ListenAndServe returned error: %s", err)
	}
}
