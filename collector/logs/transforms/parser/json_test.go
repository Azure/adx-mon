package parser

import (
	"testing"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/require"
)

func TestJsonParse(t *testing.T) {
	type testcase struct {
		name         string
		input        string
		expectedBody map[string]interface{}
		expectErr    bool
	}

	tests := []testcase{
		{
			name:         "empty",
			input:        "",
			expectedBody: map[string]interface{}{},
			expectErr:    true,
		},
		{
			name:         "invalid plain string",
			input:        "not-a-string",
			expectedBody: map[string]interface{}{},
			expectErr:    true,
		},
		{
			name:         "invalid json",
			input:        "{",
			expectedBody: map[string]interface{}{},
			expectErr:    true,
		},
		{
			name:         "valid json",
			input:        `{"a": 1, "b": "2", "c": {"d": 3}}`,
			expectedBody: map[string]interface{}{"a": 1.0, "b": "2", "c": map[string]interface{}{"d": 3.0}},
			expectErr:    false,
		},
	}

	parser, _ := NewJsonParser(JsonParserConfig{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := types.NewLog()
			err := parser.Parse(log, tt.input)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedBody, log.Body)
			}
		})
	}
}

func BenchmarkJsonParse(b *testing.B) {
	parser, _ := NewJsonParser(JsonParserConfig{})
	msg := `{"a": 1, "b": "2", "c": {"d": 3}}`
	log := types.NewLog()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		parser.Parse(log, msg)
	}
}
