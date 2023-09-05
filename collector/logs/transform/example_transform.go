package transform

import (
	"context"

	"github.com/Azure/adx-mon/collector/logs"
)

type ExampleTransform struct {
}

func (t *ExampleTransform) Open(ctx context.Context) error {
	return nil
}

func (t *ExampleTransform) Transform(ctx context.Context, batch *logs.LogBatch) ([]*logs.LogBatch, error) {
	for _, log := range batch.Logs {
		log.Attributes["example"] = "value"
	}
	batches := [1]*logs.LogBatch{batch}
	return batches[:], nil
}

func (t *ExampleTransform) Close() error {
	return nil
}
