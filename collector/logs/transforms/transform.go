package transforms

import (
	"fmt"

	"github.com/Azure/adx-mon/collector/logs/transforms/plugin"
	"github.com/Azure/adx-mon/collector/logs/transforms/plugin/addattributes"
	"github.com/Azure/adx-mon/collector/logs/types"
)

type TransformCreator func(config map[string]any) (types.Transformer, error)

const (
	TransformTypePlugin        = "plugin"
	TransformTypeAddAttributes = "addattributes"
)

var transformCreators = map[string]TransformCreator{
	TransformTypePlugin:        plugin.FromConfigMap,
	TransformTypeAddAttributes: addattributes.FromConfigMap,
}

func IsValidTransformType(transformType string) bool {
	_, ok := transformCreators[transformType]
	return ok
}

func NewTransform(transformType string, config map[string]any) (types.Transformer, error) {
	creator, ok := transformCreators[transformType]
	if !ok {
		return nil, fmt.Errorf("unknown transform type: %s", transformType)
	}

	return creator(config)
}
