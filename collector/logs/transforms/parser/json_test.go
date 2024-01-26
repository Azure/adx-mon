package parser

import (
	"testing"

	"github.com/Azure/adx-mon/collector/logs/types"
)

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
