package metrics

import (
	"context"
	"strings"
	"time"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
	srv "github.com/Azure/adx-mon/pkg/service"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/unsafe"
	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
)

type HealthReporter interface {
	IsHealthy() bool
	TransferQueueSize() int
	UploadQueueSize() int
	SegmentsTotal() int64
	SegmentsSize() int64
	UnhealthyReason() string
	MaxSegmentAge() time.Duration
}

type TimeSeriesWriter interface {
	Write(ctx context.Context, wr prompb.WriteRequest) error
}

type StatementExecutor interface {
	Database() string
	Mgmt(ctx context.Context, query kusto.Statement, options ...kusto.MgmtOption) (*kusto.RowIterator, error)
}

type Elector interface {
	IsLeader() bool
}

type Service interface {
	srv.Component
}

type ServiceOpts struct {
	Hostname         string
	Elector          Elector
	KustoCli         []StatementExecutor
	PeerHealthReport HealthReporter
}

// Service manages the collection of metrics for ingestors.
type service struct {
	closeFn context.CancelFunc

	hostname  string
	elector   Elector
	kustoClis []StatementExecutor
	health    HealthReporter
}

func NewService(opts ServiceOpts) Service {
	return &service{
		elector:   opts.Elector,
		kustoClis: opts.KustoCli,
		hostname:  opts.Hostname,
		health:    opts.PeerHealthReport,
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

	var lastCount float64
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// This is only set when running on ingestor currently.
			if s.elector != nil {
				// Only one node should execute the cardinality counts so see if we are the owner.
				if len(s.kustoClis) > 0 && s.elector.IsLeader() {
					stmt := kusto.NewStmt("", kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).UnsafeAdd(
						".set-or-append async AdxmonIngestorTableCardinalityCount <| CountCardinality",
					)

					for _, cli := range s.kustoClis {
						iter, err := cli.Mgmt(ctx, stmt)
						if err != nil {
							logger.Errorf("Failed to execute cardinality counts: %s", err)
						} else {
							iter.Stop()
						}
					}
				}
			}

			mets, err := prometheus.DefaultGatherer.Gather()
			if err != nil {
				logger.Errorf("Failed to gather metrics: %s", err)
				continue
			}

			var currentTotal, activeConns, droppedConns float64
			for _, v := range mets {
				switch *v.Type {
				case io_prometheus_client.MetricType_GAUGE:
					for _, vv := range v.Metric {
						if !strings.HasPrefix(v.GetName(), Namespace) {
							continue
						}

						if strings.Contains(v.GetName(), "ingestor_active_connections") {
							activeConns += vv.Gauge.GetValue()
						}
					}
				case io_prometheus_client.MetricType_COUNTER:
					for _, vv := range v.Metric {
						if !strings.HasPrefix(v.GetName(), Namespace) {
							continue
						}

						if strings.Contains(v.GetName(), "samples_stored_total") {
							currentTotal += vv.Counter.GetValue()
						} else if strings.Contains(v.GetName(), "ingestor_dropped_connections_total") {
							droppedConns += vv.Counter.GetValue()
						}
					}
				}
			}

			logger.Infof("Status IngestionSamplesPerSecond=%0.2f SamplesIngested=%d IsHealthy=%v "+
				"UploadQueueSize=%d TransferQueueSize=%d SegmentsTotal=%d SegmentsSize=%d UnhealthyReason=%s "+
				"ActiveConnections=%d DroppedConnections=%d MaxSegmentAgeSeconds=%0.2f",
				(currentTotal-lastCount)/60, uint64(currentTotal),
				s.health.IsHealthy(),
				s.health.UploadQueueSize(),
				s.health.TransferQueueSize(),
				s.health.SegmentsTotal(),
				s.health.SegmentsSize(),
				s.health.UnhealthyReason(),
				int(activeConns),
				int(droppedConns),
				s.health.MaxSegmentAge().Seconds())

			lastCount = currentTotal

			// Clear the gauges to prune old metrics that may not be collected anymore.
			IngestorSegmentsMaxAge.Reset()
			IngestorSegmentsSizeBytes.Reset()
			IngestorSegmentsTotal.Reset()
			LogsProxyUploaded.Reset()
		}
	}
}
