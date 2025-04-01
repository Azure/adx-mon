package addattributes

import (
	"context"
	"fmt"

	"github.com/Azure/adx-mon/collector/logs/types"
)

type Config struct {
	ResourceValues map[string]string
}

type Transform struct {
	resourceValues map[string]string
}

func NewTransform(config Config) *Transform {
	return &Transform{
		resourceValues: config.ResourceValues,
	}
}

func FromConfigMap(config map[string]interface{}) (types.Transformer, error) {
	resourceValues, ok := config["ResourceValues"].(map[string]string)
	if !ok {
		return nil, fmt.Errorf("ResourceValues is required")
	}

	return &Transform{
		resourceValues: resourceValues,
	}, nil
}

func (t *Transform) Open(ctx context.Context) error {
	return nil
}

func (t *Transform) Transform(ctx context.Context, batch *types.LogBatch) (*types.LogBatch, error) {
	for _, log := range batch.Logs {
		for key, value := range t.resourceValues {
			log.SetResourceValue(key, value)
		}
	}
	return batch, nil
}

func (t *Transform) Close() error {
	return nil
}

func (t *Transform) Name() string {
	return "AddAttributesTransform"
}
