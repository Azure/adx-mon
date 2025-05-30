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
			input:    []string{"json", "keyvalue", "klog"},
			expected: []Parser{&JsonParser{}, &KeyValueParser{}, &KlogParser{}},
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
			name:     "valid klog",
			input:    "klog",
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
	log.SetBodyValue("some-field", message)
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
		{
			name:    "successful klog parse",
			parsers: []Parser{&KlogParser{}},
			message: `I0506 15:40:33.494743       1 bounded_frequency_runner.go:296] "sync-runner: ran, next possible in 1s, periodic in 1h0m0s"`,
			expectedBody: map[string]interface{}{
				"filename":    "bounded_frequency_runner.go",
				"line_number": "296",
				"timestamp":   "15:40:33.494743",
				"message":     "sync-runner: ran, next possible in 1s, periodic in 1h0m0s",
			},
		},
		{
			name:    "klog parse with key-value pairs",
			parsers: []Parser{&KlogParser{}},
			message: `I1025 00:15:15.525108       1 controller_utils.go:116] "Pod status updated" pod="kube-system/kubedns" status="ready"`,
			expectedBody: map[string]interface{}{
				"timestamp":   "00:15:15.525108",
				"filename":    "controller_utils.go",
				"line_number": "116",
				"message":     "Pod status updated",
				"pod":         "kube-system/kubedns",
				"status":      "ready",
			},
		},
		{
			name:    "klog parse with unstructured message",
			parsers: []Parser{&KlogParser{}},
			message: `I0506 15:40:33.494743       1 bounded_frequency_runner.go:296] sync-runner: ran, next possible in 1s, periodic in 1h0m0s`,
			expectedBody: map[string]interface{}{
				"timestamp":   "15:40:33.494743",
				"filename":    "bounded_frequency_runner.go",
				"line_number": "296",
				"message":     "sync-runner: ran, next possible in 1s, periodic in 1h0m0s",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := types.NewLog()
			ExecuteParsers(tt.parsers, log, tt.message, "test")
			require.Equal(t, tt.expectedBody, log.GetBody())
		})
	}
}
