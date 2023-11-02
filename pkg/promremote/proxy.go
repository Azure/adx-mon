package promremote

import (
	"context"
	"strings"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
	"golang.org/x/sync/errgroup"
)

type RemoteWriteProxy struct {
	Client                   *Client
	Endpoints                []string
	MaxBatchSize             int
	DisableMetricsForwarding bool
}

func (r *RemoteWriteProxy) Write(ctx context.Context, _ string, wr prompb.WriteRequest) error {
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

	if r.DisableMetricsForwarding {
		return nil
	}

	g, gCtx := errgroup.WithContext(ctx)
	for i := range r.Endpoints {
		for len(wr.Timeseries) > 0 {
			var batch prompb.WriteRequest
			for j := 0; j < r.MaxBatchSize && len(wr.Timeseries) > 0; j++ {
				batch.Timeseries = append(batch.Timeseries, wr.Timeseries[0])
				wr.Timeseries = wr.Timeseries[1:]
			}

			endpoint := r.Endpoints[i]
			logger.Infof("Sending %d timeseries to %s", len(batch.Timeseries), endpoint)
			g.Go(func() error {
				return r.Client.Write(gCtx, endpoint, &batch)
			})
		}
	}
	return g.Wait()
}
