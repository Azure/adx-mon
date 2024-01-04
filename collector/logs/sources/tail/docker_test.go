package tail

import (
	"encoding/json"
	"testing"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestParseDockerLog(t *testing.T) {
	data := `{"log":"{\"msg\": \"hello world\", \"kusto.database\": \"FakeLogs\", \"kusto.table\": \"FakeTable\"}"}`
	log := &types.Log{Body: make(map[string]any), Attributes: make(map[string]any)}
	require.NoError(t, parseDockerLog(data, log))

	var msg map[string]any
	require.NoError(t, json.Unmarshal([]byte(log.Body["message"].(string)), &msg))

	require.Equal(t, "hello world", msg["msg"])
	require.Equal(t, "FakeLogs", gjson.Get(log.Body["message"].(string), `kusto\.database`).String())
}
