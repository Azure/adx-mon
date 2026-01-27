package plugin

import (
	"context"
	"testing"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/require"
)

func TestYaegi(t *testing.T) {
	transform, err := NewTransform(TransformConfig{
		GoPath:     "test-plugin",
		ImportName: "github.com/Azure/testplugin/pkg/transforms",
		Config:     map[string]any{},
	})
	require.NoError(t, err)

	messageOne := types.NewLog()
	messageOne.SetBodyValue("message", "Hello")
	messageTwo := types.NewLog()
	logBatch := &types.LogBatch{
		Logs: []*types.Log{
			messageOne,
			messageTwo,
		},
	}

	err = transform.Open(context.Background())
	require.NoError(t, err)
	output, err := transform.Transform(context.Background(), logBatch)
	require.NoError(t, err)
	require.NotNil(t, output)
	require.Equal(t, 2, len(output.Logs))
	transform.Close()
}
