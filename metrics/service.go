package metrics

import (
	"context"
	"strings"
	"time"

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
}

type ServiceOpts struct {
}

// Service manages the collection of metrics for ingestors.
type service struct {
	closeFn context.CancelFunc

	hostname string
}

func NewService(opts ServiceOpts) Service {
	return &service{}
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
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()

	var lastCount float64
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			mets, err := prometheus.DefaultGatherer.Gather()
			if err != nil {
				logger.Error("Failed to gather metrics: %s", err)
				continue
			}

			for _, v := range mets {
				switch *v.Type {
				case io_prometheus_client.MetricType_COUNTER:
					for _, vv := range v.Metric {
						if !strings.HasPrefix(v.GetName(), Namespace) {
							continue
						}

						if strings.Contains(v.GetName(), "samples_stored_total") {
							logger.Info("Ingestion rate %0.2f samples/sec, samples ingested=%d", (vv.Counter.GetValue()-lastCount)/10, uint64(vv.Counter.GetValue()))
							lastCount = vv.Counter.GetValue()
						}
					}
				}
			}
		}
	}
}
