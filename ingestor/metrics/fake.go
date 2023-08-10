package metrics

import (
	"context"
	"errors"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/samples"
)

type FakeRequestWriter struct {
	samples.Writer
}

func (f *FakeRequestWriter) Write(ctx context.Context, s interface{}) error {
	wr, ok := s.(prompb.WriteRequest)
	if !ok {
		return errors.New("invalid type")
	}
	logger.Info("Received %d samples. Dropping", len(wr.Timeseries))
	return nil
}
