package metrics

import (
	"context"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
)

type FakeRequestWriter struct {
}

func (f *FakeRequestWriter) Write(ctx context.Context, database string, wr prompb.WriteRequest) error {
	logger.Infof("Received %d samples for database %s. Dropping", len(wr.Timeseries), database)
	return nil
}
