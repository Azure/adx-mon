package metrics

import (
	"context"
	"strings"
	"time"

	"github.com/Azure/adx-mon/pkg/counter"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
	srv "github.com/Azure/adx-mon/pkg/service"
	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
)

type TimeSeriesWriter interface {
	Write(ctx context.Context, wr prompb.WriteRequest) error
}

type Service interface {
	srv.Component
	AddSeries(key string, id uint64)
}

type ServiceOpts struct {
}

// Service manages the collection of metrics for ingestors.
type service struct {
	closeFn context.CancelFunc

	estimator *counter.MultiEstimator
}

func NewService(opts ServiceOpts) Service {
	return &service{
		estimator: counter.NewMultiEstimator(),
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
			s.estimator.Roll()
			IngestorMetricsCardinality.Reset()
			for _, k := range s.estimator.Keys() {
				IngestorMetricsCardinality.WithLabelValues(k).Set(float64(s.estimator.Count(k)))
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
			IngestorSegmentsCount.Reset()
		}
	}
}
