package promremote

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
	"golang.org/x/sync/errgroup"
)

type RemoteWriteProxy struct {
	client                   *Client
	endpoints                []string
	maxBatchSize             int
	disableMetricsForwarding bool

	batches  chan prompb.WriteRequest
	cancelFn context.CancelFunc
}

func NewRemoteWriteProxy(client *Client, endpoints []string, maxBatchSize int, disableMetricsForwarding bool) *RemoteWriteProxy {
	p := &RemoteWriteProxy{
		client:                   client,
		endpoints:                endpoints,
		maxBatchSize:             maxBatchSize,
		disableMetricsForwarding: disableMetricsForwarding,
		batches:                  make(chan prompb.WriteRequest, 100),
	}
	return p
}

func (r *RemoteWriteProxy) Open(ctx context.Context) error {
	ctx, cancelFn := context.WithCancel(context.Background())
	r.cancelFn = cancelFn
	go r.flush(ctx)
	return nil
}

func (r *RemoteWriteProxy) Close() error {
	r.cancelFn()
	return nil
}

func (r *RemoteWriteProxy) Write(ctx context.Context, wr prompb.WriteRequest) error {
	if logger.IsDebug() {
		var sb strings.Builder
		for _, ts := range wr.Timeseries {
			sb.Reset()
			for i, l := range ts.Labels {
				sb.Write(l.Name)
				sb.WriteString("=")
				sb.Write(l.Value)
				if i < len(ts.Labels)-1 {
					sb.Write([]byte(","))
				}
			}
			sb.Write([]byte(" "))
			for _, s := range ts.Samples {
				logger.Debugf("%s %d %f", sb.String(), s.Timestamp, s.Value)
			}
		}
	}

	if r.disableMetricsForwarding {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case r.batches <- wr:
		return nil
	case <-time.After(5 * time.Second):
		// If the channel is full, we will try to flush the batch
		return fmt.Errorf("writes are throttled")
	}
}

func (c *RemoteWriteProxy) flush(ctx context.Context) {
	var pendingBatch prompb.WriteRequest
	for {

		select {
		case <-ctx.Done():
			return
		case b := <-c.batches:
			var nextBatch prompb.WriteRequest
			pendingBatch.Timeseries = append(pendingBatch.Timeseries, b.Timeseries...)

			// Flush as many full batches as we can
			for len(pendingBatch.Timeseries) >= c.maxBatchSize {
				nextBatch.Timeseries = nextBatch.Timeseries[:0]
				nextBatch.Timeseries = append(nextBatch.Timeseries, pendingBatch.Timeseries[c.maxBatchSize:]...)
				pendingBatch.Timeseries = pendingBatch.Timeseries[:c.maxBatchSize]
				if err := c.sendBatch(ctx, &pendingBatch); err != nil {
					logger.Errorf(err.Error())
				}
				pendingBatch = nextBatch
			}
		case <-time.After(10 * time.Second):
			var nextBatch prompb.WriteRequest
			for len(pendingBatch.Timeseries) >= c.maxBatchSize {
				nextBatch.Timeseries = nextBatch.Timeseries[:0]
				nextBatch.Timeseries = append(nextBatch.Timeseries, pendingBatch.Timeseries[c.maxBatchSize:]...)
				pendingBatch.Timeseries = pendingBatch.Timeseries[:c.maxBatchSize]
				if err := c.sendBatch(ctx, &pendingBatch); err != nil {
					logger.Errorf(err.Error())
				}
				pendingBatch = nextBatch
			}

			if err := c.sendBatch(ctx, &pendingBatch); err != nil {
				logger.Errorf(err.Error())
			}
			pendingBatch.Timeseries = pendingBatch.Timeseries[:0]
		}
	}
}

func (p *RemoteWriteProxy) sendBatch(ctx context.Context, wr *prompb.WriteRequest) error {
	if len(wr.Timeseries) == 0 {
		return nil
	}

	if len(p.endpoints) == 0 || logger.IsDebug() {
		var sb strings.Builder
		for _, ts := range wr.Timeseries {
			sb.Reset()
			for i, l := range ts.Labels {
				sb.Write(l.Name)
				sb.WriteString("=")
				sb.Write(l.Value)
				if i < len(ts.Labels)-1 {
					sb.Write([]byte(","))
				}
			}
			sb.Write([]byte(" "))
			for _, s := range ts.Samples {
				logger.Debugf("%s %d %f", sb.String(), s.Timestamp, s.Value)
			}

		}
	}

	start := time.Now()
	defer func() {
		logger.Infof("Sending %d timeseries to %d endpoints duration=%s", len(wr.Timeseries), len(p.endpoints), time.Since(start))
	}()

	g, gCtx := errgroup.WithContext(ctx)
	for _, endpoint := range p.endpoints {
		endpoint := endpoint
		g.Go(func() error {
			return p.client.Write(gCtx, endpoint, wr)
		})
	}
	return g.Wait()
}
