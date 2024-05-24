package transforms

import (
	"context"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/testplugin/pkg/mapping"
)

type AddEmoji struct {
}

func New() types.Transformer {
	return &AddEmoji{}
}

func (t *AddEmoji) Open(ctx context.Context) error {
	return nil
}

func (t *AddEmoji) Transform(ctx context.Context, batch *types.LogBatch) (*types.LogBatch, error) {
	for _, log := range batch.Logs {
		message, ok := log.Body["message"]
		if !ok {
			continue
		}
		messageStr, ok := message.(string)
		if !ok {
			continue
		}
		log.Body[types.BodyKeyMessage] = mapping.Map(messageStr)
	}
	return batch, nil
}

func (t *AddEmoji) Close() error {
	return nil
}

func (t *AddEmoji) Name() string {
	return "AddEmoji"
}
