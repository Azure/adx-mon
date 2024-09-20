package promremote

import (
	"bytes"
	"context"
	"fmt"
	"sort"
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

	// queue is the channel where incoming writes are queued.  These are arbitrary sized writes.
	queue chan *prompb.WriteRequest
	// ready is the channel where writes are batched up to maxBatchSize and ready to be sent to the remote write endpoint.
	ready chan *prompb.WriteRequest

	cancelFn context.CancelFunc
}

func NewRemoteWriteProxy(client *Client, endpoints []string, maxBatchSize int, disableMetricsForwarding bool) *RemoteWriteProxy {
	p := &RemoteWriteProxy{
		client:                   client,
		endpoints:                endpoints,
		maxBatchSize:             maxBatchSize,
		disableMetricsForwarding: disableMetricsForwarding,
		queue:                    make(chan *prompb.WriteRequest, 100),
		ready:                    make(chan *prompb.WriteRequest, 5),
	}
	return p
}

func (r *RemoteWriteProxy) Open(ctx context.Context) error {
	ctx, cancelFn := context.WithCancel(context.Background())
	r.cancelFn = cancelFn
	go r.flush(ctx)
	for i := 0; i < cap(r.ready); i++ {
		go r.sendBatch(ctx)
	}
	return nil
}

func (r *RemoteWriteProxy) Close() error {
	r.cancelFn()
	return nil
}

func (r *RemoteWriteProxy) Write(ctx context.Context, wr *prompb.WriteRequest) error {
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
	case r.queue <- wr:
		return nil
	case <-time.After(15 * time.Second):
		// If the channel is full, we will try to flush the batch
		return fmt.Errorf("writes are throttled")
	}
}

func (c *RemoteWriteProxy) flush(ctx context.Context) {
	pendingBatch := &prompb.WriteRequest{}
	for {

		select {
		case <-ctx.Done():
			return
		case b := <-c.queue:
			pendingBatch.Timeseries = append(pendingBatch.Timeseries, b.Timeseries...)

			// Flush as many full queue as we can
			for len(pendingBatch.Timeseries) >= c.maxBatchSize {
				nextBatch := prompb.WriteRequestPool.Get()
				nextBatch.Timeseries = append(nextBatch.Timeseries, pendingBatch.Timeseries[:c.maxBatchSize]...)
				pendingBatch.Timeseries = append(pendingBatch.Timeseries[:0], pendingBatch.Timeseries[c.maxBatchSize:]...)
				c.ready <- nextBatch
			}
		case <-time.After(10 * time.Second):
			for len(pendingBatch.Timeseries) >= c.maxBatchSize {
				nextBatch := prompb.WriteRequestPool.Get()
				nextBatch.Timeseries = append(nextBatch.Timeseries, pendingBatch.Timeseries[:c.maxBatchSize]...)
				pendingBatch.Timeseries = append(pendingBatch.Timeseries[:0], pendingBatch.Timeseries[c.maxBatchSize:]...)
				c.ready <- nextBatch
			}
			if len(pendingBatch.Timeseries) == 0 {
				continue
			}
			nextBatch := prompb.WriteRequestPool.Get()
			nextBatch.Timeseries = append(nextBatch.Timeseries, pendingBatch.Timeseries...)

			c.ready <- nextBatch
			pendingBatch.Reset()
		}
	}
}

func (p *RemoteWriteProxy) sendBatch(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case wr := <-p.ready:
			// This sorts all the series by metric name which has the effect of reducing contention on segments
			// in ingestor as the locks tend to be acquired in order of series name.  Since there are usually
			// multiple series with the same name, this can reduce contention.
			sort.Slice(wr.Timeseries, func(i, j int) bool {
				return bytes.Compare(wr.Timeseries[i].Labels[0].Value, wr.Timeseries[j].Labels[0].Value) < 0
			})

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
			g, gCtx := errgroup.WithContext(ctx)
			for _, endpoint := range p.endpoints {
				endpoint := endpoint
				g.Go(func() error {
					return p.client.Write(gCtx, endpoint, wr)
				})
			}
			logger.Infof("Sending %d timeseries to %d endpoints duration=%s", len(wr.Timeseries), len(p.endpoints), time.Since(start))
			if err := g.Wait(); err != nil {
				logger.Errorf("Error sending batch: %v", err)
			}
			prompb.WriteRequestPool.Put(wr)
		}
	}
}
