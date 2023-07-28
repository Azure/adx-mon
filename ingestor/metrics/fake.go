package metrics

import (
	"context"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
)

type FakeRequestWriter struct {
}

func (f *FakeRequestWriter) Write(ctx context.Context, wr prompb.WriteRequest) error {
	logger.Info("Received %d samples. Dropping", len(wr.Timeseries))
	return nil
}
