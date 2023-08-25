package metrics

import (
	"context"
	"strings"
	"time"

	"github.com/Azure/adx-mon/pkg/counter"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
	srv "github.com/Azure/adx-mon/pkg/service"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/ingest"
	"github.com/Azure/azure-kusto-go/kusto/unsafe"
	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
)

type TimeSeriesWriter interface {
	Write(ctx context.Context, wr prompb.WriteRequest) error
}

type MetricPartitioner interface {
	Owner([]byte) (string, string)
}

type Service interface {
	srv.Component
	AddSeries(key string, id uint64)
}

type ServiceOpts struct {
	Hostname    string
	Partitioner MetricPartitioner
	KustoCli    ingest.QueryClient
	Database    string
}

// Service manages the collection of metrics for ingestors.
type service struct {
	closeFn context.CancelFunc

	hostname    string
	estimator   *counter.MultiEstimator
	partitioner MetricPartitioner
	kustoCli    ingest.QueryClient
	database    string
}

func NewService(opts ServiceOpts) Service {
	return &service{
		estimator:   counter.NewMultiEstimator(),
		partitioner: opts.Partitioner,
		kustoCli:    opts.KustoCli,
		database:    opts.Database,
		hostname:    opts.Hostname,
	}
}

func (s *service) Open(ctx context.Context) error {
	ctx, s.closeFn = context.WithCancel(ctx)
	go s.collect(ctx)
	return nil
}

func (s *service) Close() error {
	s.closeFn()
	return nil
}

func (s *service) AddSeries(key string, id uint64) {
	s.estimator.Add(key, id)
}

func (s *service) collect(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()

	var lastCount float64
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// Only one node should execute the cardinality counts so see if we are the owner.
			hostname, _ := s.partitioner.Owner([]byte("AdxmonIngestorTableCardinalityCount"))
			if s.kustoCli != nil && hostname == s.hostname {
				stmt := kusto.NewStmt("", kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).UnsafeAdd(
					".set-or-append async AdxmonIngestorTableCardinalityCount <| CountCardinality",
				)
				iter, err := s.kustoCli.Mgmt(ctx, s.database, stmt)
				if err != nil {
					logger.Error("Failed to execute cardinality counts: %s", err)
				} else {
					iter.Stop()
				}
			}

			mets, err := prometheus.DefaultGatherer.Gather()
			if err != nil {
				logger.Error("Failed to gather metrics: %s", err)
				continue
			}

			currentTotal := 0.0
			for _, v := range mets {
				switch *v.Type {
				case io_prometheus_client.MetricType_COUNTER:
					for _, vv := range v.Metric {
						if !strings.HasPrefix(v.GetName(), Namespace) {
							continue
						}

						if strings.Contains(v.GetName(), "samples_stored_total") {
							currentTotal += vv.Counter.GetValue()
						}
					}
				}
			}

			logger.Info("Ingestion rate %0.2f samples/sec, samples ingested=%d", (currentTotal-lastCount)/60, uint64(currentTotal))
			lastCount = currentTotal

			// Clear the gauges to prune old metrics that may not be collected anymore.
			IngestorSegmentsMaxAge.Reset()
			IngestorSegmentsSizeBytes.Reset()
			IngestorSegmentsTotal.Reset()
		}
	}
}
