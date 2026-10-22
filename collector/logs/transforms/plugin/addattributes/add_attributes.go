package addattributes

import (
	"context"
	"fmt"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/collector/metadata"
)

type Config struct {
	ResourceValues map[string]string
	DynamicLabeler metadata.LogLabeler
}

type Transform struct {
	resourceValues map[string]string
	dynamicLabeler metadata.LogLabeler
}

func NewTransform(config Config) *Transform {
	return &Transform{
		resourceValues: config.ResourceValues,
		dynamicLabeler: config.DynamicLabeler,
	}
}

func FromConfigMap(config map[string]interface{}) (types.Transformer, error) {
	resourceValues, ok := config["ResourceValues"].(map[string]string)
	if !ok {
		return nil, fmt.Errorf("ResourceValues is required")
	}

	return &Transform{
		resourceValues: resourceValues,
		dynamicLabeler: nil, // not supported via this path
	}, nil
}

func (t *Transform) Open(ctx context.Context) error {
	return nil
}

func (t *Transform) Transform(ctx context.Context, batch *types.LogBatch) (*types.LogBatch, error) {
	for _, log := range batch.Logs {
		if t.dynamicLabeler != nil {
			t.dynamicLabeler.SetResourceValues(log)
		}

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
