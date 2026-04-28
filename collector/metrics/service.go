package metrics

import (
	"context"
	"strings"
	"time"

	adxmetrics "github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	srv "github.com/Azure/adx-mon/pkg/service"
	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
)

type Service interface {
	srv.Component
}

type ServiceOpts struct {
	PeerHealthReport adxmetrics.HealthReporter
}

// service manages the periodic Status log emission for the collector.
type service struct {
	closeFn context.CancelFunc

	health adxmetrics.HealthReporter
}

func NewService(opts ServiceOpts) Service {
	return &service{
		health: opts.PeerHealthReport,
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
			adxmetrics.LogsProxyUploaded.Reset()
		}
	}
}
