package parser

import (
	"io"
	"testing"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/require"
)

func TestNewParsers(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []Parser
	}{
		{
			name:     "valid parser",
			input:    []string{"json", "keyvalue"},
			expected: []Parser{&JsonParser{}, &KeyValueParser{}},
		},
		{
			name:     "empty",
			input:    []string{},
			expected: []Parser{},
		},
		{
			name:     "skips invalid",
			input:    []string{"invalid", "json", "invalid2"},
			expected: []Parser{&JsonParser{}},
		},
		{
			name:     "all invalid",
			input:    []string{"invalid", "invalid2"},
			expected: []Parser{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsers := NewParsers(tt.input, "test")
			require.Len(t, parsers, len(tt.expected))
			for i, parser := range parsers {
				require.IsType(t, tt.expected[i], parser)
			}
		})
	}
}

func TestIsValidParser(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid json",
			input:    "json",
			expected: true,
		},
		{
			name:     "valid keyvalue",
			input:    "keyvalue",
			expected: true,
		},
		{
			name:     "invalid",
			input:    "invalid",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, IsValidParser(tt.input))
		})
	}
}

type alwaysFailingParser struct{}

func (p *alwaysFailingParser) Parse(*types.Log, string) error {
	return io.EOF
}

type alwaysSetsAFieldParser struct{}

func (p *alwaysSetsAFieldParser) Parse(log *types.Log, message string) error {
	log.Body = map[string]interface{}{
		"some-field": message,
	}
	return nil
}

func TestExecuteParsers(t *testing.T) {
	tests := []struct {
		name         string
		parsers      []Parser
		message      string
		expectedBody map[string]interface{}
	}{
		{
			name:    "successful json parse",
			parsers: []Parser{&JsonParser{}},
			message: `{"a": 1, "b": "2", "c": {"d": 3}, "message": "hello"}`,
			expectedBody: map[string]interface{}{
				"a": 1.0,
				"b": "2",
				"c": map[string]interface{}{
					"d": 3.0,
				},
				"message": "hello",
			},
		},
		{
			name:    "successful keyvalue parse",
			parsers: []Parser{&KeyValueParser{}},
			message: `a=1.1 b="abc" c=true message="hello"`,
			expectedBody: map[string]interface{}{
				"a":       1.1,
				"b":       "abc",
				"c":       true,
				"message": "hello",
			},
		},
		{
			name:    "skips failing parser",
			parsers: []Parser{&alwaysFailingParser{}, &JsonParser{}},
			message: `{"a": 1, "b": "2", "c": {"d": 3}}`,
			expectedBody: map[string]interface{}{
				"a": 1.0,
				"b": "2",
				"c": map[string]interface{}{
					"d": 3.0,
				},
			},
		},
		{
			name:    "only executes the first successful parser",
			parsers: []Parser{&alwaysFailingParser{}, &JsonParser{}, &alwaysSetsAFieldParser{}},
			message: `{"a": 1, "b": "2", "c": {"d": 3}, "message": "hello"}`,
			expectedBody: map[string]interface{}{
				"a": 1.0,
				"b": "2",
				"c": map[string]interface{}{
					"d": 3.0,
				},
				"message": "hello",
			},
		},
		{
			name:    "invalid json parse, all parsers fail",
			parsers: []Parser{&JsonParser{}, &alwaysFailingParser{}},
			// missing closing curly brace
			message: `{"a": 1, "b": "2", "c": {"d": 3}`,
			expectedBody: map[string]interface{}{
				types.BodyKeyMessage: `{"a": 1, "b": "2", "c": {"d": 3}`,
			},
		},
		{
			name:    "plain text, json should fail and body fall back to the message field",
			parsers: []Parser{&JsonParser{}},
			message: `Hello world`,
			expectedBody: map[string]interface{}{
				types.BodyKeyMessage: `Hello world`,
			},
		},
		{
			name:    "empty parser list",
			parsers: []Parser{},
			message: `{"a": 1, "b": "2", "c": {"d": 3}}`,
			expectedBody: map[string]interface{}{
				types.BodyKeyMessage: `{"a": 1, "b": "2", "c": {"d": 3}}`,
			},
		},
		{
			name:    "json and keyvalue parsers",
			parsers: []Parser{&JsonParser{}, &KeyValueParser{}},
			message: `a=b c=d`,
			expectedBody: map[string]interface{}{
				"a": "b",
				"c": "d",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := types.NewLog()
			ExecuteParsers(tt.parsers, log, tt.message, "test")
			require.Equal(t, tt.expectedBody, log.Body)
		})
	}
}
