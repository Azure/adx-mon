package sinks

import (
	"context"
	"fmt"

	"github.com/Azure/adx-mon/collector/logs/types"
)

type StdoutSink struct {
}

func NewStdoutSink() *StdoutSink {
	return &StdoutSink{}
}

func (s *StdoutSink) Open(ctx context.Context) error {
	return nil
}

func (s *StdoutSink) Send(ctx context.Context, batch *types.LogBatch) error {
	for _, log := range batch.Logs {
		fmt.Println(log)
	}
	batch.Ack()
	return nil
}

func (s *StdoutSink) Close() error {
	return nil
}

func (s *StdoutSink) Name() string {
	return "StdoutSink"
}
