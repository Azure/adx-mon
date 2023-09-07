package sink

import (
	"context"
	"fmt"

	"github.com/Azure/adx-mon/collector/logs"
)

type StdoutSink struct {
}

func NewStdoutSink() *StdoutSink {
	return &StdoutSink{}
}

func (s *StdoutSink) Open(ctx context.Context) error {
	return nil
}

func (s *StdoutSink) Send(ctx context.Context, batch *logs.LogBatch) error {
	fmt.Println(batch.String())
	return nil
}

func (s *StdoutSink) Close() error {
	return nil
}
