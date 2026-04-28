package metrics

import (
	"context"
	"strings"
	"time"

	adxmetrics "github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	srv "github.com/Azure/adx-mon/pkg/service"
	azkustodata "github.com/Azure/azure-kusto-go/azkustodata"
	"github.com/Azure/azure-kusto-go/azkustodata/kql"
	kustov1 "github.com/Azure/azure-kusto-go/azkustodata/query/v1"
	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
)

type StatementExecutor interface {
	Database() string
	Endpoint() string
	Mgmt(ctx context.Context, query azkustodata.Statement, options ...azkustodata.QueryOption) (kustov1.Dataset, error)
}

type Elector interface {
	IsLeader() bool
}

type Service interface {
	srv.Component
}

type ServiceOpts struct {
	Elector Elector

	// MetricsKustoCli is the Kusto clients for metrics clusters.
	MetricsKustoCli []StatementExecutor

	// KustoCli is the Kusto clients for all clusters.
	KustoCli         []StatementExecutor
	PeerHealthReport adxmetrics.HealthReporter
}

// service manages the collection of metrics for ingestors.
type service struct {
	closeFn context.CancelFunc

	elector         Elector
	metricsKustoCli []StatementExecutor
	allKustoClis    []StatementExecutor
	health          adxmetrics.HealthReporter
}

func NewService(opts ServiceOpts) Service {
	return &service{
		elector:         opts.Elector,
		metricsKustoCli: opts.MetricsKustoCli,
		allKustoClis:    opts.KustoCli,
		health:          opts.PeerHealthReport,
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

func (s *service) collect(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s.elector != nil {
				// Only one node should execute the cardinality counts so see if we are the owner.
				if s.elector.IsLeader() {
					if len(s.metricsKustoCli) > 0 {
						stmt := kql.New("").AddUnsafe(
							".set-or-append async AdxmonIngestorTableCardinalityCount <| CountCardinality",
						)

						for _, cli := range s.metricsKustoCli {
							_, err := cli.Mgmt(ctx, stmt)
							if err != nil {
								logger.Errorf("Failed to execute cardinality counts: %s", err)
							}
						}
					}

					if len(s.allKustoClis) > 0 {
						stmt := kql.New("").AddUnsafe(
							".set-or-append async AdxmonIngestorTableDetails <| .show tables details" +
								"| extend PreciseTimeStamp = now() " +
								"| project PreciseTimeStamp, DatabaseName, TableName, TotalExtents, TotalExtentSize, TotalOriginalSize, TotalRowCount, HotExtents, HotExtentSize, HotOriginalSize," +
								"HotRowCount, RetentionPolicy, CachingPolicy, ShardingPolicy, MergePolicy, IngestionBatchingPolicy, MinExtentsCreationTime, MaxExtentsCreationTime, TableId",
						)

						for _, cli := range s.allKustoClis {
							_, err := cli.Mgmt(ctx, stmt)
							if err != nil {
								logger.Errorf("Failed to execute table sizes: %s", err)
							}
						}
					}
				}
			}

			mets, err := prometheus.DefaultGatherer.Gather()
			if err != nil {
				logger.Errorf("Failed to gather metrics: %s", err)
				continue
			}

			var activeConns, droppedConns float64
			for _, v := range mets {
				switch *v.Type {
				case io_prometheus_client.MetricType_GAUGE:
					for _, vv := range v.Metric {
						if !strings.HasPrefix(v.GetName(), adxmetrics.Namespace) {
							continue
						}

						if strings.Contains(v.GetName(), "ingestor_active_connections") {
							activeConns += vv.Gauge.GetValue()
						}
					}
				case io_prometheus_client.MetricType_COUNTER:
					for _, vv := range v.Metric {
						if !strings.HasPrefix(v.GetName(), adxmetrics.Namespace) {
							continue
						}

						if strings.Contains(v.GetName(), "ingestor_dropped_connections_total") {
							droppedConns += vv.Counter.GetValue()
						}
					}
				}
			}

			logger.Infof("Status IsHealthy=%v "+
				"UploadQueueSize=%d TransferQueueSize=%d SegmentsTotal=%d SegmentsSize=%d UnhealthyReason=%s "+
				"ActiveConnections=%d DroppedConnections=%d MaxSegmentAgeSeconds=%0.2f",
				s.health.IsHealthy(),
				s.health.UploadQueueSize(),
				s.health.TransferQueueSize(),
				s.health.SegmentsTotal(),
				s.health.SegmentsSize(),
				s.health.UnhealthyReason(),
				int(activeConns),
				int(droppedConns),
				s.health.MaxSegmentAge().Seconds())
		}
	}
}
