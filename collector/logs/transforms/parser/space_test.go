package parser

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Azure/adx-mon/collector/logs/types"
)

func TestSpaceParse(t *testing.T) {
	type testcase struct {
		name         string
		input        string
		expectedBody map[string]interface{}
	}

	tests := []testcase{
		{
			name:         "empty",
			input:        "",
			expectedBody: map[string]interface{}{},
		},
		{
			name:  "single word",
			input: "hello",
			expectedBody: map[string]interface{}{
				"field0": "hello",
			},
		},
		{
			name:  "multiple words",
			input: "hello world how are you",
			expectedBody: map[string]interface{}{
				"field0": "hello",
				"field1": "world",
				"field2": "how",
				"field3": "are",
				"field4": "you",
			},
		},
		{
			name:  "extra spaces",
			input: "  hello   world  ",
			expectedBody: map[string]interface{}{
				"field0": "hello",
				"field1": "world",
			},
		},
		{
			name:  "tabs",
			input: "hello\tworld",
			expectedBody: map[string]interface{}{
				"field0": "hello",
				"field1": "world",
			},
		},
		{
			name:  "multiple tabs and spaces",
			input: "hello\t\tworld  foo  bar",
			expectedBody: map[string]interface{}{
				"field0": "hello",
				"field1": "world",
				"field2": "foo",
				"field3": "bar",
			},
		},
		{
			name:  "ints",
			input: "123 456 789",
			expectedBody: map[string]interface{}{
				"field0": "123",
				"field1": "456",
				"field2": "789",
			},
		},
		{
			name:  "floats",
			input: "123.456 789.123",
			expectedBody: map[string]interface{}{
				"field0": "123.456",
				"field1": "789.123",
			},
		},
		{
			name:  "scientific notation",
			input: "3.590e-06 1.23e+10",
			expectedBody: map[string]interface{}{
				"field0": "3.590e-06",
				"field1": "1.23e+10",
			},
		},
	}

	parser, _ := NewSpaceParser(SpaceParserConfig{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := types.NewLog()
			err := parser.Parse(log, tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.expectedBody, log.GetBody())
		})
	}
}

func BenchmarkSpaceParse(b *testing.B) {
	parser, _ := NewSpaceParser(SpaceParserConfig{})
	msg := "hello world how are you today"
	log := types.NewLog()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		parser.Parse(log, msg)
	}
}
