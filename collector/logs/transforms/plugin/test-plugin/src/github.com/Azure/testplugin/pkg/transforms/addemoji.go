package transforms

import (
	"context"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/testplugin/pkg/mapping"
)

type AddEmoji struct {
	suffix string
}

func New(config map[string]any) types.Transformer {
	if config == nil {
		config = map[string]any{}
	}

	suffix := ""
	if rawSuffix, ok := config["Suffix"]; ok {
		if value, ok := rawSuffix.(string); ok {
			suffix = value
		}
	}

	return &AddEmoji{suffix: suffix}
}

func (t *AddEmoji) Open(ctx context.Context) error {
	return nil
}

func (t *AddEmoji) Transform(ctx context.Context, batch *types.LogBatch) (*types.LogBatch, error) {
	for _, log := range batch.Logs {
		message, ok := log.GetBodyValue("message")
		if !ok {
			continue
		}
		messageStr, ok := message.(string)
		if !ok {
			continue
		}
		mapped := mapping.Map(messageStr)
		if t.suffix != "" {
			mapped += t.suffix
		}
		log.SetBodyValue(types.BodyKeyMessage, mapped)
	}
	return batch, nil
}

func (t *AddEmoji) Close() error {
	return nil
}

func (t *AddEmoji) Name() string {
	return "AddEmoji"
}
