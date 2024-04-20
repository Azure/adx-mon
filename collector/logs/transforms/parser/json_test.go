package parser

import (
	"testing"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/require"
)

func TestJsonParse(t *testing.T) {
	parser, _ := NewJsonParser(JsonParserConfig{})
	log := &types.Log{
		Body: map[string]interface{}{
			types.BodyKeyMessage: `{"a": 1, "b": "2", "c": {"d": 3}}`,
		},
	}
	err := parser.Parse(log)
	require.NoError(t, err)
	require.Equal(t, 1.0, log.Body["a"])
	require.Equal(t, "2", log.Body["b"])
	require.Equal(t, map[string]interface{}{"d": 3.0}, log.Body["c"])
}

func BenchmarkJsonParse(b *testing.B) {
	parser, _ := NewJsonParser(JsonParserConfig{})
	log := &types.Log{
		Body: map[string]interface{}{
			types.BodyKeyMessage: `{"a": 1, "b": "2", "c": {"d": 3}}`,
		},
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		parser.Parse(log)
	}
}
