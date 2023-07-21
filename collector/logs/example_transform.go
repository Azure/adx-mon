package logs

import "context"

type ExampleTransform struct {
}

func (t *ExampleTransform) Init() {
}

func (t *ExampleTransform) Transform(ctx context.Context, batch *LogBatch) ([]*LogBatch, error) {
	for _, log := range batch.Logs {
		log.Attributes["example"] = "value"
	}
	batches := [1]*LogBatch{batch}
	return batches[:], nil
}

func (t *ExampleTransform) Close(ctx context.Context) error {
	return nil
}
