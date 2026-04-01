package logrouter

import (
	"context"
	"strings"

	"github.com/Azure/adx-mon/collector/logs/types"
)

const (
	// ResourceLogSources is the resource key for the adx-mon/log-sources annotation.
	// Format: sourceVal:DB:Table[,sourceVal:DB:Table,...]
	// Routes logs to a specific database and table based on the "source" field in the log body.
	ResourceLogSources = "adx-mon/log-sources"

	// ResourceLogKeys is the resource key for the adx-mon/log-keys annotation.
	// Format: key:value:DB:Table[,key:value:DB:Table,...]
	// Routes logs to a specific database and table based on arbitrary key/value matches in the log body.
	ResourceLogKeys = "adx-mon/log-keys"
)

// Transform is a built-in log transform that routes logs to different
// databases and tables based on pod annotations.
//
// It supports two annotation-driven routing mechanisms:
//
//   - adx-mon/log-sources: Routes based on the "source" field in the log body.
//     Each entry maps a source value to a destination database:table pair.
//
//   - adx-mon/log-keys: Routes based on an arbitrary body key matching a
//     specific value. Each entry maps a key:value pair to a destination
//     database:table pair.
//
// Log-sources is evaluated first. If a match is found, log-keys is skipped.
type Transform struct{}

func NewTransform() *Transform {
	return &Transform{}
}

func FromConfigMap(config map[string]interface{}) (types.Transformer, error) {
	return &Transform{}, nil
}

func (t *Transform) Open(ctx context.Context) error {
	return nil
}

func (t *Transform) Close() error {
	return nil
}

func (t *Transform) Name() string {
	return "LogRouterTransform"
}

func (t *Transform) Transform(ctx context.Context, batch *types.LogBatch) (*types.LogBatch, error) {
	for _, log := range batch.Logs {
		t.route(log)
	}
	return batch, nil
}

func (t *Transform) route(log *types.Log) {
	if log == nil {
		return
	}

	// Try log-sources first: match on the "source" body field.
	if sources := types.StringOrEmpty(log.GetResourceValue(ResourceLogSources)); sources != "" {
		sourceBody := types.StringOrEmpty(log.GetBodyValue("source"))
		for entry := range strings.SplitSeq(sources, ",") {
			parts := strings.Split(entry, ":")
			if len(parts) != 3 {
				continue
			}
			if sourceBody == parts[0] {
				log.SetAttributeValue(types.AttributeDatabaseName, parts[1])
				log.SetAttributeValue(types.AttributeTableName, parts[2])
				return
			}
		}
	}

	// Try log-keys: match on an arbitrary body key=value pair.
	if keys := types.StringOrEmpty(log.GetResourceValue(ResourceLogKeys)); keys != "" {
		for entry := range strings.SplitSeq(keys, ",") {
			parts := strings.Split(entry, ":")
			if len(parts) != 4 {
				continue
			}
			if types.StringOrEmpty(log.GetBodyValue(parts[0])) == parts[1] {
				log.SetAttributeValue(types.AttributeDatabaseName, parts[2])
				log.SetAttributeValue(types.AttributeTableName, parts[3])
				return
			}
		}
	}
}
