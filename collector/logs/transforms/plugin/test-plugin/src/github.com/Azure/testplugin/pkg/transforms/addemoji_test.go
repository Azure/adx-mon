package transforms

import (
	"context"
	"testing"

	"github.com/Azure/adx-mon/collector/logs/types"
)

func TestAddEmoji(t *testing.T) {
	transformer := New(map[string]any{
		"Suffix": " ✅",
	})
	if transformer.Name() != "AddEmoji" {
		t.Errorf("Expected name to be AddEmoji, got %s", transformer.Name())
	}

	logWithMessage := types.NewLog()
	logWithMessage.SetBodyValue("message", "Hello World")
	logWithoutMessage := types.NewLog()
	logWithMessageOfNonStringType := types.NewLog()
	logWithMessageOfNonStringType.SetBodyValue("message", 123)
	batch := &types.LogBatch{
		Logs: []*types.Log{
			logWithMessage,
			logWithoutMessage,
			logWithMessageOfNonStringType,
		},
	}

	transformer.Open(context.Background())
	defer transformer.Close()
	transformedBatch, err := transformer.Transform(context.Background(), batch)
	if err != nil {
		t.Errorf("Expected no error, got %s", err)
	}
	if len(transformedBatch.Logs) != 3 {
		t.Errorf("Expected 3 log, got %d", len(transformedBatch.Logs))
	}
	message := types.StringOrEmpty(transformedBatch.Logs[0].GetBodyValue("message"))
	if message != "Hello World 😃 ✅" {
		t.Errorf("Expected message to be Hello World 😃 ✅, got %s", message)
	}
}
