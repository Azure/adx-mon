package main

import (
	"context"
	"fmt"
	promingest "github.com/Azure/adx-mon"
	"github.com/Azure/adx-mon/adx"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/adx-mon/storage"
	"github.com/urfave/cli/v2"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
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
	storageDir = ctx.String("storage-dir")
	kustoEndpoint = ctx.String("kusto-endpoint")
	if !strings.Contains(kustoEndpoint, "=") {
		return fmt.Errorf("invalid kusto endpoint: %s", kustoEndpoint)
	}

	split := strings.Split(kustoEndpoint, "=")
	database = split[0]
	kustoEndpoint = split[1]

	concurrentUploads = ctx.Int("uploads")
	maxSegmentSize = ctx.Int64("max-segment-size")
	maxSegmentAge = ctx.Duration("max-segment-age")
	useCliAuth = ctx.Bool("use-cli-auth")
	staticColumns = ctx.StringSlice("labels")

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
	return nil
}
